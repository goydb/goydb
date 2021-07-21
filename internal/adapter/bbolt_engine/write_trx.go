package bbolt_engine

import (
	"fmt"

	"log"

	"github.com/goydb/goydb/pkg/port"
	"go.etcd.io/bbolt"
)

var _ port.EngineWriteTransaction = (*WriteTransaction)(nil)

type opCode int

const (
	opEnsureBucket opCode = iota
	opDeleteBucket
	opPut
	opPutWithSequence
	opDelete
)

type op struct {
	code             opCode
	arg1, arg2, arg3 []byte
	keyWithSeq       port.KeyWithSeq
}

// WriteTransaction will store all write operations
// in a log an execute them all at once in a transaction.
// The aim is to unblock write transactions to the database
// by packing the transactions into a log.
type WriteTransaction struct {
	ReadTransaction
	opLog []op
}

func NewWriteTransaction(readTx *bbolt.Tx) *WriteTransaction {
	return &WriteTransaction{
		ReadTransaction: ReadTransaction{
			tx: readTx,
		},
	}
}

func (t *WriteTransaction) EnsureBucket(bucket []byte) {
	t.opLog = append(t.opLog, op{
		code: opEnsureBucket,
		arg1: bucket,
	})
}

func (t *WriteTransaction) DeleteBucket(bucket []byte) {
	t.opLog = append(t.opLog, op{
		code: opDeleteBucket,
		arg1: bucket,
	})
}

func (t *WriteTransaction) Put(bucket, k, v []byte) {
	t.opLog = append(t.opLog, op{
		code: opPut,
		arg1: bucket,
		arg2: k,
		arg3: v,
	})
}

func (t *WriteTransaction) PutWithSequence(bucket, k, v []byte, fn port.KeyWithSeq) {
	t.opLog = append(t.opLog, op{
		code:       opPutWithSequence,
		arg1:       bucket,
		arg2:       k,
		arg3:       v,
		keyWithSeq: fn,
	})
}

func (t *WriteTransaction) Delete(bucket, k []byte) {
	t.opLog = append(t.opLog, op{
		code: opDelete,
		arg1: bucket,
		arg2: k,
	})
}

func (t *WriteTransaction) Commit(tx *bbolt.Tx) error {
	for _, op := range t.opLog {
		var err error
		switch op.code {
		case opEnsureBucket:
			log.Printf("OP ensure bucket %q", op.arg1)
			_, err = tx.CreateBucketIfNotExists(op.arg1)
		case opDeleteBucket:
			log.Printf("OP delete bucket %q", op.arg1)
			err = tx.DeleteBucket(op.arg1)
		case opPut:
			log.Printf("OP put %q (%d) to %q", op.arg2, len(op.arg3), op.arg1)
			b := tx.Bucket(op.arg1)
			if b == nil {
				return fmt.Errorf("failed to put %q to bucket %q: no bucket", string(op.arg2), string(op.arg1))
			}
			err = b.Put(op.arg2, op.arg3)
		case opPutWithSequence:
			b := tx.Bucket(op.arg1)
			if b == nil {
				return fmt.Errorf("failed to put %q to bucket %q: no bucket", string(op.arg2), string(op.arg1))
			}
			var seq uint64
			seq, err = b.NextSequence()
			if err == nil {
				nk, nv := op.keyWithSeq(op.arg2, seq)
				if nk == nil { // key not changed
					nk = op.arg2
				}
				if nv == nil { // value not changed
					nv = op.arg3
				}
				log.Printf("OP put with seq %q (%d) to %q", nk, len(nv), op.arg1)
				err = b.Put(nk, nv)
			}
		case opDelete:
			b := tx.Bucket(op.arg1)
			if b != nil {
				log.Printf("OP delete %q from %q", op.arg2, op.arg1)
				err = b.Delete(op.arg2)
			}
		default:
			panic(fmt.Errorf("invalid opcode: %d", op.code))
		}
		if err != nil {
			return err
		}
	}
	return nil
}
