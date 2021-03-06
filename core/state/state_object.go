// Copyright 2014 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package state

import (
	"io"
	"math/big"

	"github.com/MinterTeam/minter-go-node/core/types"
	"github.com/MinterTeam/minter-go-node/rlp"
	"sort"
)

// stateObject represents an Ethereum account which is being modified.
//
// The usage pattern is as follows:
// First you need to obtain a state object.
// Account values can be accessed and modified through the object.
// Finally, call CommitTrie to write the modified storage trie into a database.
type stateObject struct {
	address types.Address
	data    Account

	// Cache flags.
	// When an object is marked suicided it will be delete from the trie
	// during the "update" phase of the state transition.
	suicided bool
	touched  bool
	deleted  bool
	onDirty  func(addr types.Address) // Callback method to mark a state object newly dirty
}

// empty returns whether the account is considered empty.
func (s *stateObject) empty() bool {
	return false
	//return s.data.Nonce == 0 && s.data.Balance.Sign() == 0
}

type Balances struct {
	Data map[types.CoinSymbol]*big.Int
}

type Balance struct {
	Coin   types.CoinSymbol
	Amount *big.Int
}

func (b Balances) getCoins() []types.CoinSymbol {
	keys := make([]types.CoinSymbol, 0, len(b.Data))
	for k, v := range b.Data {
		if v.Cmp(types.Big0) > 0 {
			keys = append(keys, k)
		}
	}

	sort.Slice(keys, func(a, b int) bool {
		return big.NewInt(0).SetBytes(keys[a].Bytes()).Cmp(big.NewInt(0).SetBytes(keys[b].Bytes())) == 1
	})

	return keys
}

func (b Balances) EncodeRLP(w io.Writer) error {

	var balances []Balance

	for _, k := range b.getCoins() {
		balances = append(balances, Balance{k, b.Data[k]})
	}

	return rlp.Encode(w, balances)
}

func (b *Balances) DecodeRLP(s *rlp.Stream) error {

	if b.Data == nil {
		b.Data = make(map[types.CoinSymbol]*big.Int)
	}

	var balances []Balance

	if err := s.Decode(&balances); err != nil {
		return err
	}

	for _, v := range balances {
		b.Data[v.Coin] = v.Amount
	}

	return nil
}

// Account is the Ethereum consensus representation of accounts.
// These objects are stored in the main account trie.
type Account struct {
	Nonce   uint64
	Balance Balances
	Root    types.Hash // merkle root of the storage trie
}

// newObject creates a state object.
func newObject(db *StateDB, address types.Address, data Account, onDirty func(addr types.Address)) *stateObject {
	if data.Balance.Data == nil {
		data.Balance.Data = make(map[types.CoinSymbol]*big.Int)
	}

	return &stateObject{
		address: address,
		data:    data,
		onDirty: onDirty,
	}
}

// EncodeRLP implements rlp.Encoder.
func (c *stateObject) EncodeRLP(w io.Writer) error {
	return rlp.Encode(w, c.data)
}

func (self *stateObject) markSuicided() {
	self.suicided = true
	if self.onDirty != nil {
		self.onDirty(self.Address())
		self.onDirty = nil
	}
}

func (c *stateObject) touch() {
	if c.onDirty != nil {
		c.onDirty(c.Address())
		c.onDirty = nil
	}
	c.touched = true
}

// AddBalance removes amount from c's balance.
// It is used to add funds to the destination account of a transfer.
func (c *stateObject) AddBalance(coinSymbol types.CoinSymbol, amount *big.Int) {
	// EIP158: We must check emptiness for the objects such that the account
	// clearing (0,0,0 objects) can take effect.
	if amount.Sign() == 0 {
		if c.empty() {
			c.touch()
		}

		return
	}
	c.SetBalance(coinSymbol, new(big.Int).Add(c.Balance(coinSymbol), amount))
}

// SubBalance removes amount from c's balance.
// It is used to remove funds from the origin account of a transfer.
func (c *stateObject) SubBalance(coinSymbol types.CoinSymbol, amount *big.Int) {
	if amount.Sign() == 0 {
		return
	}
	c.SetBalance(coinSymbol, new(big.Int).Sub(c.Balance(coinSymbol), amount))
}

func (self *stateObject) SetBalance(coinSymbol types.CoinSymbol, amount *big.Int) {
	EmitBalanceChange(self.address, coinSymbol, amount)
	self.setBalance(coinSymbol, amount)
}

func (self *stateObject) setBalance(coinSymbol types.CoinSymbol, amount *big.Int) {

	if self.data.Balance.Data == nil {
		self.data.Balance.Data = make(map[types.CoinSymbol]*big.Int)
	}

	self.data.Balance.Data[coinSymbol] = amount
	if self.onDirty != nil {
		self.onDirty(self.Address())
		self.onDirty = nil
	}
}

// Return the gas back to the origin. Used by the Virtual machine or Closures
func (c *stateObject) ReturnGas(gas *big.Int) {}

func (self *stateObject) deepCopy(db *StateDB, onDirty func(addr types.Address)) *stateObject {
	stateObject := newObject(db, self.address, self.data, onDirty)
	stateObject.suicided = self.suicided
	stateObject.deleted = self.deleted
	return stateObject
}

//
// Attribute accessors
//

// Returns the address of the contract/account
func (c *stateObject) Address() types.Address {
	return c.address
}

func (self *stateObject) SetNonce(nonce uint64) {
	self.setNonce(nonce)
}

func (self *stateObject) setNonce(nonce uint64) {
	self.data.Nonce = nonce
	if self.onDirty != nil {
		self.onDirty(self.Address())
		self.onDirty = nil
	}
}

func (self *stateObject) Balance(coinSymbol types.CoinSymbol) *big.Int {

	if self.data.Balance.Data == nil {
		return big.NewInt(0)
	}

	if self.data.Balance.Data[coinSymbol] == nil {
		return big.NewInt(0)
	}

	return self.data.Balance.Data[coinSymbol]
}

func (self *stateObject) Balances() Balances {
	return self.data.Balance
}

func (self *stateObject) Nonce() uint64 {
	return self.data.Nonce
}
