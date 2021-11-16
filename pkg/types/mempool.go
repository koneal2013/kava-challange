package types

import (
	"fmt"
	"github.com/koneal2013/go-sortedmap"
	"github.com/pkg/errors"
	"mempool/pkg/logging"
	"os"
	"sync"
)

var (
	maxMempoolSize int
	logger         logging.LoggingSystem
	txChan         chan *Tx
)

const (
	ERR_MEMPOOL_SIZE = "mempool size cannot be less than or equal to 0"
)

type memsync struct {
	*sync.Once
	*sync.Mutex
}

type mempool struct {
	sync         *memsync
	Transactions *sortedmap.SortedMap
}

type MempoolI interface {
	AddTx(tx *Tx) (err error)
}

func NewMempool(maxPoolSize int, ls logging.LoggingSystem) *mempool {
	if maxPoolSize <= 0 {
		ls.Fatal(ERR_MEMPOOL_SIZE)
	}
	maxMempoolSize = maxPoolSize
	logger = ls
	return &mempool{
		sync: &memsync{
			Once:  &sync.Once{},
			Mutex: &sync.Mutex{},
		},
		Transactions: sortedmap.New(maxMempoolSize, compareTx),
	}
}

func (mp mempool) AddTx(tx *Tx, group *sync.WaitGroup) (err error) {
	logger.Sugar().Named("mempool/AddTx").Debugf("calculating total fee for transaction with hash [%s]", tx.TxHash)
	tx.calculateTotalFees()
	mp.sync.Do(func() {
		txChan = make(chan *Tx, 100)
		go func() {
			err := mp.processTx(group)
			if err != nil {
				return
			}
		}()
		go func() {
			err := mp.processTx(group)
			if err != nil {
				return
			}
		}()
		go func() {
			err := mp.processTx(group)
			if err != nil {
				return
			}
		}()
	})
	txChan <- tx
	return nil
}
func (mp mempool) processTx(wg *sync.WaitGroup) (err error) {
	wg.Add(1)
	defer wg.Done()
	for {
		//when mempool is full, prioritize transactions with higher fee
		mp.sync.Lock()
		if mp.Transactions.Len() == maxMempoolSize {
			txToBeDeleted, _ := mp.Transactions.Get(mp.Transactions.GetSortedKeyByIndex(mp.Transactions.Len() - 1))
			txHashToDelete, _ := mp.Transactions.BoundedKeys(txToBeDeleted, txToBeDeleted)
			if err = mp.dropTx(txHashToDelete[0].(string)); err != nil {
				errors.Wrapf(err, "unable to add transaction with hash [%s] because the mempool is full", txHashToDelete[0])
			}
		}
		mp.sync.Unlock()
		transaction, ok := <-txChan
		if !ok {
			fmt.Println("ok is false")
			return
		}
		mp.sync.Lock()
		if !mp.Transactions.Insert(transaction.TxHash, transaction) {
			logger.Sugar().Named("mempool/AddTx").Debugf("Transaction with hash [%s] already exists", transaction.TxHash)
		}
		mp.sync.Unlock()
	}
}

func (mp mempool) dropTx(txHash string) (err error) {
	if tx, exists := mp.Transactions.Get(txHash); exists {
		logger.Sugar().Named("mempool/dropTx").Debugf("dropping low priority transaction with hash [%s] and total fee of [%v]", tx.(*Tx).TxHash, tx.(*Tx).TotalFee)
		mp.Transactions.Delete(txHash)
		return nil
	}
	return errors.Errorf("Tranaction with hash [%s] doesn't exist in mempool", txHash)
}

func compareTx(i interface{}, j interface{}) bool {
	_, iok := i.(*Tx)
	_, jok := j.(*Tx)
	if !iok || !jok {
		panic("incompatible types")
	}
	return i.(*Tx).TotalFee > j.(*Tx).TotalFee
}

func (mp mempool) ExportToFile() (err error) {
	if prioritizedMempoolFile, err := os.Create("prioritized-transactions.txt"); err != nil {
		logger.Sugar().Named("mempool/ExportToFile").Error("unable to create file [prioritized-transactions.txt]")
		return err
	} else {
		defer prioritizedMempoolFile.Close()
		mp.sync.Lock()
		sortedTxs, _ := mp.Transactions.BatchGet(mp.Transactions.Keys())
		mp.sync.Unlock()
		for _, tx := range sortedTxs {
			if _, err = prioritizedMempoolFile.WriteString(fmt.Sprintf("TxHash=%v Gas=%v FeePerGas=%v Signature=%v TotalFee=%v \n", tx.(*Tx).TxHash, tx.(*Tx).Gas, tx.(*Tx).FeePerGas, tx.(*Tx).Signature, tx.(*Tx).TotalFee)); err != nil {
				logger.Sugar().Named("mempool/ExportToFile").Errorf("unable to write [TxHash=%v Gas=%v FeePerGas=%v Signature=%v] to prioritized-transactions.txt", tx.(*Tx).TxHash, tx.(*Tx).Gas, tx.(*Tx).FeePerGas, tx.(*Tx).Signature)
				continue
			}
		}
	}
	return nil
}

func (mp mempool) CloseTxInsertChan() {
	close(txChan)
}
