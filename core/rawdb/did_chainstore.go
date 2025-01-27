package rawdb

import (
	"bytes"
	"errors"
	"strings"
	"time"

	elaCom "github.com/elastos/Elastos.ELA/common"
	"github.com/elastos/Elastos.ELA/utils/http"

	"github.com/elastos/Elastos.ELA.SideChain/service"

	"github.com/elastos/Elastos.ELA.SideChain.ETH/common"
	"github.com/elastos/Elastos.ELA.SideChain.ETH/core/types"
	"github.com/elastos/Elastos.ELA.SideChain.ETH/core/vm/did"
	"github.com/elastos/Elastos.ELA.SideChain.ETH/ethdb"
	"github.com/elastos/Elastos.ELA.SideChain.ETH/params"
)

type EntryPrefix byte

const (
	IX_ISDID                             EntryPrefix = 0x88
	IX_DeactivateCustomizedDID           EntryPrefix = 0x89
	IX_VerifiableCredentialExpiresHeight EntryPrefix = 0x90
	IX_VerifiableCredentialTXHash        EntryPrefix = 0x91
	IX_VerifiableCredentialPayload       EntryPrefix = 0x92
	IX_CUSTOMIZEDDIDPayload              EntryPrefix = 0x93
	IX_CUSTOMIZEDDIDTXHash               EntryPrefix = 0x94
	IX_DIDTXHash                         EntryPrefix = 0x95
	IX_DIDPayload                        EntryPrefix = 0x96
	IX_DIDExpiresHeight                  EntryPrefix = 0x97
	IX_DIDDeactivate                     EntryPrefix = 0x98
	IX_CUSTOMIZEDDIDExpiresHeight        EntryPrefix = 0x99
)
var (
	ERR_READ_TX = errors.New("read transaction error")
	ERR_READ_RECEIPT = errors.New("read receipt error")
	ERR_NOT_DIDRECEIPT = errors.New("receipt is not contain did")
	ERR_NOT_DEACTIVATERECEIPT = errors.New("receipt is not contain deactivate tx")
)

func PersistRegisterDIDTx(db ethdb.KeyValueStore, log *types.DIDLog, blockHeight uint64,
	blockTimeStamp uint64) error {
	var err error
	var buffer *bytes.Reader
	operation := new(did.DIDPayload)
	buffer = bytes.NewReader(log.Data)
	err = operation.Deserialize(buffer, did.DIDVersion)
	if err != nil {
		return err
	}
	idKey := []byte(operation.DIDDoc.ID)
	expiresHeight, err := TryGetExpiresHeight(operation.DIDDoc.Expires, blockHeight, blockTimeStamp)
	if err != nil {
		return err
	}
	if err := persistRegisterDIDExpiresHeight(db, idKey, expiresHeight); err != nil {
		return err
	}
	thash, err := elaCom.Uint256FromBytes(log.TxHash.Bytes())
	if err != nil {
		return err
	}
	if err := persistRegisterDIDTxHash(db, idKey, *thash); err != nil {
		return err
	}
	// didPayload is persisted in receipt
	//if err := persistRegisterDIDPayload(db, *thash, operation); err != nil {
	//	return err
	//}

	isDID := uint64(0)
	if did.IsDID(operation.DIDDoc.ID, operation.DIDDoc.PublicKey) {
		isDID = 1
	}
	if err := persistIsDID(db, idKey, isDID); err != nil {
		return err
	}
	return nil
}

func persistIsDID(db ethdb.KeyValueStore, idKey []byte, isDID uint64) error {
	key := []byte{byte(IX_ISDID)}
	key = append(key, idKey...)

	buf := new(bytes.Buffer)
	if err := elaCom.WriteVarUint(buf, isDID); err != nil {
		return err
	}
	return db.Put(key, buf.Bytes())
}

func IsDID(db ethdb.KeyValueStore, did string) (bool, error) {
	idKey := new(bytes.Buffer)
	idKey.WriteString(did)

	key := []byte{byte(IX_ISDID)}
	key = append(key, idKey.Bytes()...)

	data, err := db.Get(key)
	if err != nil {
		return false, err
	}
	r := bytes.NewReader(data)
	// get the count of expires height
	isDID, err := elaCom.ReadVarUint(r, 0)
	if err != nil {
		return false, err
	}
	if isDID == 1 {
		return true, nil
	}
	return false, nil
}

func TryGetExpiresHeight(Expires string, blockHeight uint64, blockTimeStamp uint64) (uint64, error) {
	expiresTime, err := time.Parse(time.RFC3339, Expires)
	if err != nil {
		return 0, errors.New("invalid Expires")
	}

	var timeSpanSec, expiresSec uint64
	expiresSec = uint64(expiresTime.Unix())
	timeSpanSec = expiresSec - blockTimeStamp

	if expiresSec < blockTimeStamp {
		timeSpanSec = 0
	}
	//needsBlocks := timeSpanSec / (2 * 60)
	needsBlocks := timeSpanSec / 5
	expiresHeight := blockHeight + needsBlocks
	return expiresHeight, nil
}


func persistRegisterDIDExpiresHeight(db ethdb.KeyValueStore, idKey []byte,
	expiresHeight uint64) error {
	key := []byte{byte(IX_DIDExpiresHeight)}
	key = append(key, idKey...)
	data, err := db.Get(key)
	if err != nil {
		// when not exist, only put the current expires height into db.
		buf := new(bytes.Buffer)
		if err := elaCom.WriteVarUint(buf, 1); err != nil {
			return err
		}
		if err := elaCom.WriteUint64(buf, expiresHeight); err != nil {
			return err
		}

		return db.Put(key, buf.Bytes())
	}

	// when exist, should add current expires height to the end of the list.
	r := bytes.NewReader(data)
	count, err := elaCom.ReadVarUint(r, 0)
	if err != nil {
		return err
	}
	count++

	buf := new(bytes.Buffer)

	// write count
	if err := elaCom.WriteVarUint(buf, count); err != nil {
		return err
	}
	if err := elaCom.WriteUint64(buf, expiresHeight); err != nil {
		return err
	}
	if _, err := r.WriteTo(buf); err != nil {
		return err
	}

	return db.Put(key, buf.Bytes())
}

func persistRegisterDIDTxHash(db ethdb.KeyValueStore, idKey []byte, txHash elaCom.Uint256) error {
	key := []byte{byte(IX_DIDTXHash)}
	key = append(key, idKey...)

	data, err := db.Get(key)
	if err != nil {
		// when not exist, only put the current payload hash into db.
		buf := new(bytes.Buffer)
		if err := elaCom.WriteVarUint(buf, 1); err != nil {
			return err
		}

		if err := txHash.Serialize(buf); err != nil {
			return err
		}
		return db.Put(key, buf.Bytes())
	}

	// when exist, should add current payload hash to the end of the list.
	r := bytes.NewReader(data)
	count, err := elaCom.ReadVarUint(r, 0)
	if err != nil {
		return err
	}
	count++

	buf := new(bytes.Buffer)

	// write count
	if err := elaCom.WriteVarUint(buf, count); err != nil {
		return err
	}

	// write current payload hash
	if err := txHash.Serialize(buf); err != nil {
		return err
	}

	// write old hashes
	if _, err := r.WriteTo(buf); err != nil {
		return err
	}

	return db.Put(key, buf.Bytes())
}

func GetLastDIDTxData(db ethdb.KeyValueStore, idKey []byte, config *params.ChainConfig) (*did.DIDTransactionData, error) {
	key := []byte{byte(IX_DIDTXHash)}
	key = append(key, idKey...)

	data, err := db.Get(key)
	if err != nil {
		return nil, err
	}

	r := bytes.NewReader(data)
	count, err := elaCom.ReadVarUint(r, 0)
	if err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, errors.New("not exist")
	}
	var txHash elaCom.Uint256
	if err := txHash.Deserialize(r); err != nil {
		return nil, err
	}

	thash := common.BytesToHash(txHash.Bytes())
	recp, _, _,_ := ReadReceipt(db.(ethdb.Database), thash, config)
	if recp == nil {
		if recps := ReadRawReceipts(db.(ethdb.Database), thash, 0); recps != nil {
			if recps.Len() > 0 {
				recp = recps[0]
			}
		}
	}

	if recp == nil {
		return nil, ERR_READ_RECEIPT
	}
	if recp.DIDLog.DID == "" {
		return nil, ERR_NOT_DIDRECEIPT
	}
	tempOperation := new(did.DIDPayload)
	r = bytes.NewReader(recp.DIDLog.Data)
	err = tempOperation.Deserialize(r, did.DIDVersion)
	if err != nil {
		return nil, http.NewError(int(service.ResolverInternalError),
			"tempOperation Deserialize failed")
	}
	tempTxData := new(did.DIDTransactionData)
	tempTxData.TXID = txHash.String()
	tempTxData.Operation = *tempOperation
	tempTxData.Timestamp = tempOperation.DIDDoc.Expires

	return tempTxData, nil
}

func GetDeactivatedTxData(db ethdb.KeyValueStore, idKey []byte, config *params.ChainConfig) (*did.DIDTransactionData, error) {
	key := []byte{byte(IX_DIDDeactivate)}
	key = append(key, idKey...)

	data, err := db.Get(key)
	if err != nil {
		return nil, err
	}

	r := bytes.NewReader(data)
	var txHash elaCom.Uint256
	if err := txHash.Deserialize(r); err != nil {
		return nil, err
	}

	thash := common.BytesToHash(txHash.Bytes())
	recp, _, _,_ := ReadReceipt(db.(ethdb.Database), thash, config)
	if recp == nil {
		if recps := ReadRawReceipts(db.(ethdb.Database), thash, 0); recps != nil {
			if recps.Len() > 0 {
				recp = recps[0]
			}
		}
	}

	if recp == nil {
		return nil, ERR_READ_RECEIPT
	}
	if recp.DIDLog.DID == "" {
		return nil, ERR_NOT_DEACTIVATERECEIPT
	}

	tempOperation := new(did.DIDPayload)
	r = bytes.NewReader(recp.DIDLog.Data)
	err = tempOperation.Deserialize(r, did.DIDVersion)
	if err != nil {
		return nil, errors.New("[DIDPayload], tempOperation Deserialize failed")
	}
	tempTxData := new(did.DIDTransactionData)
	tempTxData.TXID = txHash.String()
	tempTxData.Operation = *tempOperation
	return tempTxData, nil
}

func IsDIDDeactivated(db ethdb.KeyValueStore, did string) bool {
	idKey := new(bytes.Buffer)
	idKey.WriteString(did)

	key := []byte{byte(IX_DIDDeactivate)}
	key = append(key, idKey.Bytes()...)

	_, err := db.Get(key)
	if err != nil {
		return false
	}
	return true
}

func GetAllDIDTxTxData(db ethdb.KeyValueStore, idKey []byte, config *params.ChainConfig) ([]did.DIDTransactionData, error) {
	key := []byte{byte(IX_DIDTXHash)}
	key = append(key, idKey...)

	data, err := db.Get(key)
	if err != nil {
		return nil, err
	}

	r := bytes.NewReader(data)
	count, err := elaCom.ReadVarUint(r, 0)
	if err != nil {
		return nil, err
	}
	var transactionsData []did.DIDTransactionData
	for i := uint64(0); i < count; i++ {
		var txHash elaCom.Uint256
		if err := txHash.Deserialize(r); err != nil {
			return nil, err
		}
		thash := common.BytesToHash(txHash.Bytes())
		if txn, _, _, _ := ReadTransaction(db.(ethdb.Database), common.BytesToHash(txHash.Bytes())); txn == nil {
			return nil, ERR_READ_TX
		}

		recp, _, _,_ := ReadReceipt(db.(ethdb.Database), thash, config)
		if recp == nil {
			return nil, ERR_READ_RECEIPT
		}

		if recp.DIDLog.DID == "" {
			return nil, ERR_NOT_DIDRECEIPT
		}
		tempOperation := new(did.DIDPayload)
		r := bytes.NewReader(recp.DIDLog.Data)
		err = tempOperation.Deserialize(r, did.DIDVersion)
		if err != nil {
			return nil, http.NewError(int(service.InvalidTransaction),
				"payloaddid Deserialize failed")
		}
		tempTxData := new(did.DIDTransactionData)
		tempTxData.TXID = txHash.String()
		tempTxData.Operation = *tempOperation
		tempTxData.Timestamp = tempOperation.DIDDoc.Expires
		transactionsData = append(transactionsData, *tempTxData)
	}

	return transactionsData, nil
}

func GetLastCustomizedDIDTxData(db ethdb.KeyValueStore, idKey []byte) (*did.DIDTransactionData, error) {
	key := []byte{byte(IX_CUSTOMIZEDDIDTXHash)}
	key = append(key, idKey...)

	data, err := db.Get(key)
	if err != nil {
		return nil, err
	}
	r := bytes.NewReader(data)
	count, err := elaCom.ReadVarUint(r, 0)
	if err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, errors.New("not exist")
	}
	var txHash elaCom.Uint256
	if err := txHash.Deserialize(r); err != nil {
		return nil, err
	}

	keyPayload := []byte{byte(IX_CUSTOMIZEDDIDPayload)}
	keyPayload = append(keyPayload, txHash.Bytes()...)

	dataPayload, err := db.Get(keyPayload)
	if err != nil {
		return nil, err
	}

	tempOperation := new(did.DIDPayload)
	r = bytes.NewReader(dataPayload)
	err = tempOperation.Deserialize(r, did.DIDVersion)
	if err != nil {
		return nil, http.NewError(int(service.ResolverInternalError),
			"DIDPayload Deserialize failed")
	}
	tempTxData := new(did.DIDTransactionData)
	tempTxData.TXID = txHash.String()
	tempTxData.Operation = *tempOperation
	tempTxData.Timestamp = tempOperation.GetDIDDoc().Expires

	return tempTxData, nil
}


func persistRegisterDIDPayload(db ethdb.KeyValueStore, txHash elaCom.Uint256, p *did.DIDPayload) error {
	key := []byte{byte(IX_DIDPayload)}
	key = append(key, txHash.Bytes()...)

	buf := new(bytes.Buffer)
	p.Serialize(buf, did.DIDVersion)
	return db.Put(key, buf.Bytes())
}

func IsURIHasPrefix(id string) bool {
	return strings.HasPrefix(id, did.DID_ELASTOS_PREFIX)
}

func PersistDeactivateDIDTx(db ethdb.KeyValueStore, log *types.DIDLog, thash common.Hash) error {
	key := []byte{byte(IX_DIDDeactivate)}
	idKey := []byte(log.DID)
	key = append(key, idKey...)

	buf := new(bytes.Buffer)
	txHash, err := elaCom.Uint256FromBytes(thash.Bytes())
	if err != nil {
		return err
	}
	if err := txHash.Serialize(buf); err != nil {
		return err
	}
	return db.Put(key, buf.Bytes())
}

func GetAllVerifiableCredentialTxData(db ethdb.KeyValueStore, idKey []byte, config *params.ChainConfig) ([]did.VerifiableCredentialTxData, error) {
	key := []byte{byte(IX_VerifiableCredentialTXHash)}
	key = append(key, idKey...)

	data, err := db.Get(key)
	if err != nil {
		return nil, err
	}

	r := bytes.NewReader(data)
	count, err := elaCom.ReadVarUint(r, 0)
	if err != nil {
		return nil, err
	}
	var transactionsData []did.VerifiableCredentialTxData
	for i := uint64(0); i < count; i++ {
		var txHash elaCom.Uint256
		if err := txHash.Deserialize(r); err != nil {
			return nil, err
		}
		//keyPayload := []byte{byte(IX_VerifiableCredentialPayload)}
		//keyPayload = append(keyPayload, txHash.Bytes()...)
		thash := common.BytesToHash(txHash.Bytes())
		recp, _, _,_ := ReadReceipt(db.(ethdb.Database), thash, config)
		if recp == nil {
			if recps := ReadRawReceipts(db.(ethdb.Database), thash, 0); recps != nil {
				if recps.Len() > 0 {
					recp = recps[0]
				}
			}
		}

		if recp == nil {
			return nil, ERR_READ_RECEIPT
		}
		if recp.DIDLog.DID == "" {
			return nil, ERR_NOT_DIDRECEIPT
		}
		vcPayload := new(did.DIDPayload)
		r := bytes.NewReader(recp.DIDLog.Data)
		err = vcPayload.Deserialize(r, did.VerifiableCredentialVersion)
		if err != nil {
			return nil, http.NewError(int(service.InvalidTransaction),
				"verifiable credential payload Deserialize failed")
		}
		tempTxData := new(did.VerifiableCredentialTxData)
		tempTxData.TXID = txHash.String()
		tempTxData.Timestamp = vcPayload.CredentialDoc.ExpirationDate
		tempTxData.Operation = *vcPayload
		transactionsData = append(transactionsData, *tempTxData)
	}

	return transactionsData, nil
}


func GetLastVerifiableCredentialTxData(db ethdb.KeyValueStore, idKey []byte, config *params.ChainConfig) (*did.DIDTransactionData, error) {
	key := []byte{byte(IX_VerifiableCredentialTXHash)}
	key = append(key, idKey...)

	data, err := db.Get(key)
	if err != nil {
		return nil, err
	}

	r := bytes.NewReader(data)
	count, err := elaCom.ReadVarUint(r, 0)
	if err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, errors.New("not exist")
	}
	var txHash elaCom.Uint256
	if err := txHash.Deserialize(r); err != nil {
		return nil, err
	}

	//keyPayload := []byte{byte(IX_VerifiableCredentialPayload)}
	//keyPayload = append(keyPayload, txHash.Bytes()...)

	thash := common.BytesToHash(txHash.Bytes())
	recp, _, _,_ := ReadReceipt(db.(ethdb.Database), thash, config)
	if recp == nil {
		if recps := ReadRawReceipts(db.(ethdb.Database), thash, 0); recps != nil {
			if recps.Len() > 0 {
				recp = recps[0]
			}
		}
	}

	if recp == nil {
		return nil, ERR_READ_RECEIPT
	}
	if recp.DIDLog.DID == "" {
		return nil, ERR_NOT_DIDRECEIPT
	}

	credentialPayload := new(did.DIDPayload)
	r = bytes.NewReader(recp.DIDLog.Data)
	err = credentialPayload.Deserialize(r, did.VerifiableCredentialVersion)
	if err != nil {
		return nil, http.NewError(int(service.ResolverInternalError),
			"tempOperation Deserialize failed")
	}
	tempTxData := new(did.DIDTransactionData)
	tempTxData.TXID = txHash.String()
	tempTxData.Operation = *credentialPayload
	tempTxData.Timestamp = credentialPayload.CredentialDoc.ExpirationDate

	return tempTxData, nil
}

func DeleteDIDLog(db ethdb.KeyValueStore, didLog *types.DIDLog) error {
	if didLog == nil  {
		return errors.New("didLog is nil")
	}
	id := didLog.DID
	if id == "" {
		return errors.New("invalid regPayload.DIDDoc.ID")
	}
	switch didLog.Operation {
	case did.Create_DID_Operation, did.Update_DID_Operation, did.Transfer_DID_Operation:
		if err := rollbackRegisterDIDLog(db, []byte(id), didLog.TxHash); err != nil {
			return err
		}
	case did.Deactivate_DID_Operation:
		if err := rollbackDeactivateDIDTx(db, []byte(id)); err != nil {
			return err
		}
	case did.Declare_Verifiable_Credential_Operation, did.Revoke_Verifiable_Credential_Operation:
		if err := rollbackVerifiableCredentialTx(db, []byte(id), didLog.TxHash); err != nil {
			return err
		}
	}
	return nil
}

//rollbackVerifiableCredentialTx
func rollbackVerifiableCredentialTx(db ethdb.KeyValueStore, credentialIDKey []byte, thash common.Hash) error {
	key := []byte{byte(IX_VerifiableCredentialTXHash)}
	key = append(key, credentialIDKey...)

	data, err := db.Get(key)
	if err != nil {
		return err
	}

	r := bytes.NewReader(data)
	// get the count of tx hashes
	count, err := elaCom.ReadVarUint(r, 0)
	if err != nil {
		return err
	}
	if count == 0 {
		return errors.New("not exist")
	}
	// get the newest tx hash
	var txHash elaCom.Uint256
	if err := txHash.Deserialize(r); err != nil {
		return err
	}
	// if not rollback the newest tx hash return error
	hash, err := elaCom.Uint256FromBytes(thash.Bytes())
	if err != nil {
		return err
	}
	if !txHash.IsEqual(*hash) {
		return errors.New("not rollback the last one")
	}

	//rollback operation (payload)
	keyPayload := []byte{byte(IX_VerifiableCredentialPayload)}
	keyPayload = append(keyPayload, txHash.Bytes()...)
	db.Delete(keyPayload)

	//rollback expires height
	err = rollbackVerifiableCredentialExpiresHeight(db, credentialIDKey)
	if err != nil {
		return err
	}

	if count == 1 {
		return db.Delete(key)
	}

	buf := new(bytes.Buffer)
	// write count
	if err := elaCom.WriteVarUint(buf, count-1); err != nil {
		return err
	}
	// write old hashes
	if _, err := r.WriteTo(buf); err != nil {
		return err
	}
	return db.Put(key, buf.Bytes())
}

func rollbackVerifiableCredentialExpiresHeight(db ethdb.KeyValueStore,
	credentialIDKey []byte) error {

	key := []byte{byte(IX_VerifiableCredentialExpiresHeight)}
	key = append(key, credentialIDKey...)

	data, err := db.Get(key)
	if err != nil {
		return err
	}

	r := bytes.NewReader(data)
	// get the count of expires height
	count, err := elaCom.ReadVarUint(r, 0)
	if err != nil {
		return err
	}
	if count == 0 {
		return errors.New("not exist")
	}

	if _, err = elaCom.ReadUint64(r); err != nil {
		return err
	}

	if count == 1 {
		return db.Delete(key)
	}

	buf := new(bytes.Buffer)

	// write count
	if err := elaCom.WriteVarUint(buf, count-1); err != nil {
		return err
	}

	// write old expires height
	if _, err := r.WriteTo(buf); err != nil {
		return err
	}

	return db.Put(key, buf.Bytes())
}

func rollbackRegisterDIDLog(db ethdb.KeyValueStore, idKey []byte, txhash common.Hash) error {

	key := []byte{byte(IX_DIDTXHash)}
	key = append(key, idKey...)

	data, err := db.Get(key)
	if err != nil {
		return err
	}

	r := bytes.NewReader(data)
	// get the count of tx hashes
	count, err := elaCom.ReadVarUint(r, 0)
	if err != nil {
		return err
	}
	if count == 0 {
		return errors.New("not exist")
	}

	// get the newest tx hash
	var txHash elaCom.Uint256
	if err := txHash.Deserialize(r); err != nil {
		return err
	}
	// if not rollback the newest tx hash return error
	hash, err := elaCom.Uint256FromBytes(txhash.Bytes())
	if err != nil {
		return err
	}
	if !txHash.IsEqual(*hash) {
		return errors.New("not rollback the last one")
	}

	//keyPayload := []byte{byte(IX_DIDPayload)}
	//keyPayload = append(keyPayload, txHash.Bytes()...)
	//db.Delete(keyPayload)

	//rollback expires height
	err = rollbackRegisterDIDExpiresHeight(db, idKey)
	if err != nil {
		return err
	}

	if count == 1 {
		return db.Delete(key)
	}

	buf := new(bytes.Buffer)
	// write count
	if err := elaCom.WriteVarUint(buf, count-1); err != nil {
		return err
	}
	// write old hashes
	if _, err := r.WriteTo(buf); err != nil {
		return err
	}
	return db.Put(key, buf.Bytes())
}

func rollbackDeactivateDIDTx(db ethdb.KeyValueStore, idKey []byte) error {
	key := []byte{byte(IX_DIDDeactivate)}
	key = append(key, idKey...)

	_, err := db.Get(key)
	if err != nil {
		return err
	}
	db.Delete(key)
	return nil
}

func rollbackIsDID(db ethdb.KeyValueStore,
	idKey []byte) error {
	key := []byte{byte(IX_ISDID)}
	key = append(key, idKey...)

	_, err := db.Get(key)
	if err != nil {
		return err
	}
	db.Delete(key)
	return nil
}

func rollbackRegisterDIDExpiresHeight(db ethdb.KeyValueStore, idKey []byte) error {
	key := []byte{byte(IX_DIDExpiresHeight)}
	key = append(key, idKey...)

	data, err := db.Get(key)
	if err != nil {
		return err
	}

	r := bytes.NewReader(data)
	// get the count of expires height
	count, err := elaCom.ReadVarUint(r, 0)
	if err != nil {
		return err
	}
	if count == 0 {
		return errors.New("not exist")
	}

	if _, err = elaCom.ReadUint64(r); err != nil {
		return err
	}

	if count == 1 {
		return db.Delete(key)
	}

	buf := new(bytes.Buffer)

	// write count
	if err := elaCom.WriteVarUint(buf, count-1); err != nil {
		return err
	}

	// write old expires height
	if _, err := r.WriteTo(buf); err != nil {
		return err
	}

	return db.Put(key, buf.Bytes())
}

//persistVerifiableCredentialTx
func PersistVerifiableCredentialTx(db ethdb.KeyValueStore, log *types.DIDLog,
	blockHeight uint64, blockTimeStamp uint64, thash common.Hash) error {
	var err error
	var buffer *bytes.Reader
	payload := new(did.DIDPayload)
	buffer = bytes.NewReader(log.Data)
	err = payload.Deserialize(buffer, did.DIDVersion)
	if err != nil {
		return err
	}
	id := payload.CredentialDoc.ID
	idKey := []byte(id)

	verifyCred := payload.CredentialDoc
	expiresHeight, err := TryGetExpiresHeight(verifyCred.ExpirationDate, blockHeight, blockTimeStamp)
	if err != nil {
		return err
	}

	if err := persistVerifiableCredentialExpiresHeight(db, idKey, expiresHeight); err != nil {
		return err
	}
	txhash, err := elaCom.Uint256FromBytes(thash.Bytes())
	if err != nil {
		return err
	}
	if err := persisterifiableCredentialTxHash(db, idKey, txhash); err != nil {
		return err
	}
	//didPayload is persisted in receipt
	//if err := persistVerifiableCredentialPayload(db, txhash, payload); err != nil {
	//	return err
	//}

	return nil
}

func persistVerifiableCredentialExpiresHeight(db ethdb.KeyValueStore,
	idKey []byte, expiresHeight uint64) error {
	key := []byte{byte(IX_VerifiableCredentialExpiresHeight)}
	key = append(key, idKey...)

	data, err := db.Get(key)
	if err != nil {
		// when not exist, only put the current expires height into db.
		buf := new(bytes.Buffer)
		if err := elaCom.WriteVarUint(buf, 1); err != nil {
			return err
		}
		if err := elaCom.WriteUint64(buf, expiresHeight); err != nil {
			return err
		}

		return db.Put(key, buf.Bytes())
	}

	// when exist, should add current expires height to the end of the list.
	r := bytes.NewReader(data)
	count, err := elaCom.ReadVarUint(r, 0)
	if err != nil {
		return err
	}
	count++

	buf := new(bytes.Buffer)

	// write count
	if err := elaCom.WriteVarUint(buf, count); err != nil {
		return err
	}
	if err := elaCom.WriteUint64(buf, expiresHeight); err != nil {
		return err
	}
	if _, err := r.WriteTo(buf); err != nil {
		return err
	}

	return db.Put(key, buf.Bytes())
}

func persisterifiableCredentialTxHash(db ethdb.KeyValueStore,
	idKey []byte, txHash *elaCom.Uint256) error {
	key := []byte{byte(IX_VerifiableCredentialTXHash)}
	key = append(key, idKey...)

	data, err := db.Get(key)
	if err != nil {
		// when not exist, only put the current payload hash into db.
		buf := new(bytes.Buffer)
		if err := elaCom.WriteVarUint(buf, 1); err != nil {
			return err
		}

		if err := txHash.Serialize(buf); err != nil {
			return err
		}

		return db.Put(key, buf.Bytes())
	}

	// when exist, should add current payload hash to the end of the list.
	r := bytes.NewReader(data)
	count, err := elaCom.ReadVarUint(r, 0)
	if err != nil {
		return err
	}
	count++

	buf := new(bytes.Buffer)

	// write count
	if err := elaCom.WriteVarUint(buf, count); err != nil {
		return err
	}

	// write current payload hash
	if err := txHash.Serialize(buf); err != nil {
		return err
	}

	// write old hashes
	if _, err := r.WriteTo(buf); err != nil {
		return err
	}

	return db.Put(key, buf.Bytes())
}

func persistVerifiableCredentialPayload(db ethdb.KeyValueStore,
	txHash *elaCom.Uint256, p *did.DIDPayload) error {
	key := []byte{byte(IX_VerifiableCredentialPayload)}
	key = append(key, txHash.Bytes()...)

	buf := new(bytes.Buffer)
	p.Serialize(buf, did.VerifiableCredentialVersion)
	return db.Put(key, buf.Bytes())
}
