package bc

import (
	"bytes"
	"database/sql/driver"
	"io"
	"time"

	"chain/crypto/hash256"
	"chain/encoding/blockchain"
	"chain/errors"
)

// Block describes a complete block, including its header
// and the transactions it contains.
type Block struct {
	BlockHeader
	Transactions []*Tx
}

func (b *Block) Scan(val interface{}) error {
	buf, ok := val.([]byte)
	if !ok {
		return errors.New("Scan must receive a byte slice")
	}
	r := &errors.Reader{R: bytes.NewReader(buf)}
	b.readFrom(r)
	return r.Err
}

func (b *Block) Value() (driver.Value, error) {
	buf := new(bytes.Buffer)
	_, err := b.WriteTo(buf)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (b *Block) readFrom(r *errors.Reader) {
	b.BlockHeader.readFrom(r)
	for n := blockchain.ReadUvarint(r); n > 0; n-- {
		var data TxData
		data.readFrom(r)
		// TODO(kr): store/reload hashes;
		// don't compute here if not necessary.
		tx := NewTx(data)
		b.Transactions = append(b.Transactions, tx)
	}
}

// WriteTo satisfies interface io.WriterTo.
func (b *Block) WriteTo(w io.Writer) (int64, error) {
	return b.writeTo(w, false)
}

func (b *Block) writeTo(w io.Writer, forSigning bool) (int64, error) {
	ew := errors.NewWriter(w)
	b.BlockHeader.writeTo(ew, forSigning)
	if !forSigning {
		blockchain.WriteUvarint(ew, uint64(len(b.Transactions)))
		for _, tx := range b.Transactions {
			tx.WriteTo(ew)
		}
	}
	return ew.Written(), ew.Err()
}

// Block version to use when creating new blocks.
const NewBlockVersion = 1

// BlockHeader describes necessary metadata of the block.
type BlockHeader struct {
	// Version of the block.
	Version uint32

	// Height of the block in the block chain.
	// Genesis block has height 0.
	Height uint64

	// Hash of the previous block in the block chain.
	PreviousBlockHash Hash

	// Commitment is the collection of state commitments
	// this block includes. Currently this is made of
	// the TxRoot and the StateRoot.
	Commitment []byte

	// Time of the block in seconds.
	// Must grow monotonically and can be equal
	// to the time in the previous block.
	Timestamp uint64

	// Signature script authenticates the block against
	// the output script from the previous block.
	SignatureScript []byte

	// Output script specifies a predicate for signing the next block.
	OutputScript []byte
}

// Time returns the time represented by the Timestamp in bh.
func (bh *BlockHeader) Time() time.Time {
	return time.Unix(int64(bh.Timestamp), 0).UTC()
}

func (bh *BlockHeader) Scan(val interface{}) error {
	buf, ok := val.([]byte)
	if !ok {
		return errors.New("Scan must receive a byte slice")
	}
	r := &errors.Reader{R: bytes.NewReader(buf)}
	bh.readFrom(r)
	return r.Err
}

func (bh *BlockHeader) Value() (driver.Value, error) {
	buf := new(bytes.Buffer)
	_, err := bh.WriteTo(buf)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Hash returns complete hash of the block header.
func (bh *BlockHeader) Hash() Hash {
	h := hash256.New()
	bh.WriteTo(h) // error is impossible
	var v [32]byte
	h.Sum(v[:0])
	return v
}

// HashForSig returns a hash of the block header with signature script blanked out.
// This hash is used for signing the block and verifying the signature.
func (bh *BlockHeader) HashForSig() Hash {
	h := hash256.New()
	bh.WriteForSigTo(h) // error is impossible
	var v [32]byte
	h.Sum(v[:0])
	return v
}

// TxRoot returns the transaction merkle root
// in the block Commitment field.
func (bh *BlockHeader) TxRoot() Hash {
	var hash Hash
	if len(bh.Commitment) >= 32 {
		copy(hash[:], bh.Commitment[0:32])
	}
	return hash
}

// SetTxRoot sets the transaction merkle root
// in the block Commitment field.
func (bh *BlockHeader) SetTxRoot(h Hash) {
	if len(bh.Commitment) < 32 {
		bh.Commitment = make([]byte, 32)
	}
	copy(bh.Commitment[0:32], h[:])
}

// StateRoot returns the state merkle root
// in the block Commitment field.
func (bh *BlockHeader) StateRoot() Hash {
	var hash Hash
	if len(bh.Commitment) >= 64 {
		copy(hash[:], bh.Commitment[32:64])
	}
	return hash
}

// SetStateRoot sets the state merkle root
// in the block Commitment field.
func (bh *BlockHeader) SetStateRoot(h Hash) {
	if len(bh.Commitment) < 64 {
		newComm := make([]byte, 64)
		copy(newComm, bh.Commitment)
		bh.Commitment = newComm
	}
	copy(bh.Commitment[32:64], h[:])
}

func (bh *BlockHeader) readFrom(r *errors.Reader) {
	bh.Version = blockchain.ReadUint32(r)
	bh.Height = blockchain.ReadUint64(r)
	io.ReadFull(r, bh.PreviousBlockHash[:])
	blockchain.ReadBytes(r, &bh.Commitment)
	bh.Timestamp = blockchain.ReadUint64(r)
	blockchain.ReadBytes(r, (*[]byte)(&bh.SignatureScript))
	blockchain.ReadBytes(r, (*[]byte)(&bh.OutputScript))
}

// WriteTo satisfies interface io.WriterTo.
func (bh *BlockHeader) WriteTo(w io.Writer) (int64, error) {
	ew := errors.NewWriter(w)
	bh.writeTo(ew, false)
	return ew.Written(), ew.Err()
}

// WriteForSigTo writes bh to w in a format suitable for signing.
func (bh *BlockHeader) WriteForSigTo(w io.Writer) (int64, error) {
	ew := errors.NewWriter(w)
	bh.writeTo(ew, true)
	return ew.Written(), ew.Err()
}

// writeTo writes bh to w.
// If forSigning is true, it writes an empty string instead of the signature script.
func (bh *BlockHeader) writeTo(w *errors.Writer, forSigning bool) {
	blockchain.WriteUint32(w, bh.Version)
	blockchain.WriteUint64(w, bh.Height)
	w.Write(bh.PreviousBlockHash[:])
	blockchain.WriteBytes(w, bh.Commitment)
	blockchain.WriteUint64(w, bh.Timestamp)
	if forSigning {
		blockchain.WriteBytes(w, nil)
	} else {
		blockchain.WriteBytes(w, bh.SignatureScript)
	}
	blockchain.WriteBytes(w, bh.OutputScript)
}