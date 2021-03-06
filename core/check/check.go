package check

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/MinterTeam/minter-go-node/core/types"
	"github.com/MinterTeam/minter-go-node/crypto"
	"github.com/MinterTeam/minter-go-node/crypto/sha3"
	"github.com/MinterTeam/minter-go-node/rlp"
	"math/big"
)

var (
	ErrInvalidSig = errors.New("invalid transaction v, r, s values")
)

type Check struct {
	Nonce    uint64
	DueBlock uint64
	Coin     types.CoinSymbol
	Value    *big.Int
	Lock     *big.Int
	V        *big.Int
	R        *big.Int
	S        *big.Int
}

func (check *Check) Sender() (types.Address, error) {
	return recoverPlain(check.Hash(), check.R, check.S, check.V)
}

func (check *Check) LockPubKey() ([]byte, error) {

	hash := rlpHash([]interface{}{
		check.Nonce,
		check.DueBlock,
		check.Coin,
		check.Value,
	})

	pub, err := crypto.Ecrecover(hash[:], check.Lock.Bytes())
	if err != nil {
		return nil, err
	}
	if len(pub) == 0 || pub[0] != 4 {
		return nil, errors.New("invalid public key")
	}

	return pub, nil
}

func (check *Check) Hash() types.Hash {
	return rlpHash([]interface{}{
		check.Nonce,
		check.DueBlock,
		check.Coin,
		check.Value,
		check.Lock,
	})
}

func (check *Check) String() string {
	sender, _ := check.Sender()

	return fmt.Sprintf("Check sender: %s nonce: %d, dueBlock: %d, value: %d %s", sender.String(), check.Nonce, check.DueBlock, check.Value, check.Coin.String())
}

func DecodeFromBytes(buf []byte) (*Check, error) {

	var check Check
	rlp.Decode(bytes.NewReader(buf), &check)

	if check.S == nil || check.R == nil || check.V == nil {
		return nil, errors.New("incorrect tx signature")
	}

	return &check, nil
}

func rlpHash(x interface{}) (h types.Hash) {
	hw := sha3.NewKeccak256()
	rlp.Encode(hw, x)
	hw.Sum(h[:0])
	return h
}

func recoverPlain(sighash types.Hash, R, S, Vb *big.Int) (types.Address, error) {
	if Vb.BitLen() > 8 {
		return types.Address{}, ErrInvalidSig
	}
	V := byte(Vb.Uint64() - 27)
	if !crypto.ValidateSignatureValues(V, R, S) {
		return types.Address{}, ErrInvalidSig
	}
	// encode the snature in uncompressed format
	r, s := R.Bytes(), S.Bytes()
	sig := make([]byte, 65)
	copy(sig[32-len(r):32], r)
	copy(sig[64-len(s):64], s)
	sig[64] = V
	// recover the public key from the snature
	pub, err := crypto.Ecrecover(sighash[:], sig)
	if err != nil {
		return types.Address{}, err
	}
	if len(pub) == 0 || pub[0] != 4 {
		return types.Address{}, errors.New("invalid public key")
	}
	var addr types.Address
	copy(addr[:], crypto.Keccak256(pub[1:])[12:])
	return addr, nil
}
