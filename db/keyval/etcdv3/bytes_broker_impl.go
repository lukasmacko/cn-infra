// Copyright (c) 2017 Cisco and/or its affiliates.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at:
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package etcdv3

import (
	"time"

	"github.com/coreos/etcd/clientv3"
	"github.com/coreos/etcd/clientv3/namespace"
	"github.com/coreos/etcd/mvcc/mvccpb"
	"github.com/ligato/cn-infra/db/keyval"
	"github.com/ligato/cn-infra/logging"
	"github.com/ligato/cn-infra/logging/logrus"
	"golang.org/x/net/context"
)

// BytesConnectionEtcd encapsulates the connection to etcd. It provides API to read/edit and watch values from etcd.
type BytesConnectionEtcd struct {
	etcdClient *clientv3.Client
	lessor     clientv3.Lease

	// closeCh is a channel closed when Close method is called.
	// It is leveraged to stop go routines.
	closeCh chan struct{}
}

// BytesBrokerWatcherEtcd uses BytesConnectionEtcd to access the datastore. The connection can be shared among multiple BytesBrokerWatcherEtcd.
// In case of accessing a particular subtree in etcd only, BytesBrokerWatcherEtcd allows to define a keyPrefix that is prepended to all keys
// in its methods in order to shorten keys used in arguments.
type BytesBrokerWatcherEtcd struct {
	closeCh chan struct{}
	lessor  clientv3.Lease
	kv      clientv3.KV
	watcher clientv3.Watcher
}

// bytesKeyValIterator is an iterator returned by ListValues call
type bytesKeyValIterator struct {
	index int
	len   int
	resp  *clientv3.GetResponse
}

// bytesKeyIterator is an iterator returned by ListKeys call
type bytesKeyIterator struct {
	index int
	len   int
	resp  *clientv3.GetResponse
	db    *BytesConnectionEtcd
}

// bytesKeyVal represents a single key-value pair
type bytesKeyVal struct {
	key      string
	value    []byte
	revision int64
}

var log logging.Logger

func init() {
	log = logrus.StandardLogger()
}

// SetLogger sets a logger that will be used for library logging.
func SetLogger(l logging.Logger) {
	log = l
}

// NewEtcdConnectionWithBytes creates new connection to etcd based on the given config file.
func NewEtcdConnectionWithBytes(config clientv3.Config) (*BytesConnectionEtcd, error) {
	etcdClient, err := clientv3.New(config)
	if err != nil {
		log.Errorf("Failed to connect to Etcd etcd(s) %v, Error: '%s'", config.Endpoints, err)
		return nil, err
	}
	return NewEtcdConnectionUsingClient(etcdClient)
}

// NewEtcdConnectionUsingClient creates a new instance of BytesConnectionEtcd using the provided
// etcdv3 client
func NewEtcdConnectionUsingClient(etcdClient *clientv3.Client) (*BytesConnectionEtcd, error) {
	log.Debug("NewEtcdConnectionWithBytes", etcdClient)

	conn := BytesConnectionEtcd{}
	conn.etcdClient = etcdClient
	conn.closeCh = make(chan struct{})
	conn.lessor = clientv3.NewLease(etcdClient)
	return &conn, nil
}

// Close closes the connection to ETCD.
func (db *BytesConnectionEtcd) Close() error {
	close(db.closeCh)
	if db.etcdClient != nil {
		return db.etcdClient.Close()
	}
	return nil
}

// NewBroker creates a new instance of a proxy that provides
// access to etcd. BytesConnectionEtcd is used to access the etcd
// Prefix will be prepend to key argument in all calls on created BytesBrokerWatcherEtcd. To avoid
// using a prefix pass keyval.Root constant as argument.
func (db *BytesConnectionEtcd) NewBroker(prefix string) keyval.BytesBroker {
	return &BytesBrokerWatcherEtcd{kv: namespace.NewKV(db.etcdClient, prefix), lessor: db.lessor, watcher: namespace.NewWatcher(db.etcdClient, prefix), closeCh: db.closeCh}
}

// NewWatcher creates a new instance of a proxy that provides
// access to etcd. BytesConnectionEtcd is used to access the etcd.
// Prefix will be prepend to key argument in all calls on created BytesBrokerWatcherEtcd. To avoid
// using a prefix pass keyval.Root constant as argument.
func (db *BytesConnectionEtcd) NewWatcher(prefix string) keyval.BytesWatcher {
	return &BytesBrokerWatcherEtcd{kv: namespace.NewKV(db.etcdClient, prefix), lessor: db.lessor, watcher: namespace.NewWatcher(db.etcdClient, prefix), closeCh: db.closeCh}
}

// Put calls Put function of BytesConnectionEtcd. KeyPrefix defined in constructor is prepended to key argument.
func (pdb *BytesBrokerWatcherEtcd) Put(key string, data []byte, opts ...keyval.PutOption) error {
	return putInternal(pdb.kv, pdb.lessor, key, data, opts)
}

// NewTxn creates new transaction. KeyPrefix defined in constructor will be prepended to all key arguments in the transaction.
func (pdb *BytesBrokerWatcherEtcd) NewTxn() keyval.BytesTxn {
	return newTxnInternal(pdb.kv)
}

// GetValue calls GetValue function of BytesConnectionEtcd. KeyPrefix defined in constructor is prepended to key argument.
func (pdb *BytesBrokerWatcherEtcd) GetValue(key string) (data []byte, found bool, revision int64, err error) {
	return getValueInternal(pdb.kv, key)
}

// ListValues calls ListValues function of BytesConnectionEtcd. KeyPrefix defined in constructor is prepended to key argument. The prefix
// is removed from the keys of the returned values.
func (pdb *BytesBrokerWatcherEtcd) ListValues(key string) (keyval.BytesKeyValIterator, error) {
	return listValuesInternal(pdb.kv, key)
}

// ListValuesRange calls ListValuesRange function of BytesConnectionEtcd. KeyPrefix defined in constructor is prepended to the arguments. The prefix
// is removed from the keys of the returned values.
func (pdb *BytesBrokerWatcherEtcd) ListValuesRange(fromPrefix string, toPrefix string) (keyval.BytesKeyValIterator, error) {
	return listValuesRangeInternal(pdb.kv, fromPrefix, toPrefix)
}

// ListKeys calls ListKeys function of BytesConnectionEtcd. KeyPrefix defined in constructor is prepended to the argument.
func (pdb *BytesBrokerWatcherEtcd) ListKeys(prefix string) (keyval.BytesKeyIterator, error) {
	return listKeysInternal(pdb.kv, prefix)
}

// Delete calls delete function of BytesConnectionEtcd. KeyPrefix defined in constructor is prepended to the key argument.
func (pdb *BytesBrokerWatcherEtcd) Delete(key string) (bool, error) {
	return deleteInternal(pdb.kv, key)
}

// Watch starts subscription for changes associated with the selected keys. KeyPrefix defined in constructor is prepended to all
// keys in the argument list. The prefix is removed from the keys used in watch events. Watch events will be delivered to respChan.
func (pdb *BytesBrokerWatcherEtcd) Watch(respChan chan keyval.BytesWatchResp, keys ...string) error {
	var err error
	for _, k := range keys {
		err = watchInternal(pdb.watcher, pdb.closeCh, k, respChan)
		if err != nil {
			break
		}
	}
	return err
}

func handleWatchEvent(respChan chan keyval.BytesWatchResp, ev *clientv3.Event) {

	var resp keyval.BytesWatchResp
	if ev.Type == mvccpb.DELETE {
		resp = NewBytesWatchDelResp(string(ev.Kv.Key), ev.Kv.ModRevision)
	} else if ev.IsCreate() || ev.IsModify() {
		if ev.Kv.Value != nil {
			resp = NewBytesWatchPutResp(string(ev.Kv.Key), ev.Kv.Value, ev.Kv.ModRevision)
			log.Debug("NewBytesWatchPutResp")
		}
	}
	if resp != nil {
		select {
		case respChan <- resp:
		case <-time.After(defaultTimeout):
			log.Warn("Unable to deliver watch event before timeout.")
		}
	}

}

// NewTxn creates a new transaction. A transaction can
// holds multiple operations that are all committed to the data
// store together. After a transaction has been created, one or
// more operations (put or delete) can be added to the transaction
// before it is committed.
func (db *BytesConnectionEtcd) NewTxn() keyval.BytesTxn {
	return newTxnInternal(db.etcdClient)
}

func newTxnInternal(kv clientv3.KV) keyval.BytesTxn {
	tx := bytesTxn{}
	tx.kv = kv
	return &tx
}

// Watch starts subscription for changes associated with the selected keys. Watch events will be delivered to respChan.
func (db *BytesConnectionEtcd) Watch(respChan chan keyval.BytesWatchResp, keys ...string) error {
	var err error
	for _, k := range keys {
		err = watchInternal(db.etcdClient, db.closeCh, k, respChan)
		if err != nil {
			break
		}
	}
	return err
}

// watchInternal starts the watch subscription for key.
func watchInternal(watcher clientv3.Watcher, closeCh chan struct{}, key string, respChan chan keyval.BytesWatchResp) error {

	recvChan := watcher.Watch(context.Background(), key, clientv3.WithPrefix(), clientv3.WithPrevKV())

	go func() {
		for {
			select {
			case wresp := <-recvChan:
				for _, ev := range wresp.Events {
					handleWatchEvent(respChan, ev)
				}
			case <-closeCh:
				log.WithField("key", key).Debug("Watch ended")
				return
			}
		}
	}()
	return nil
}

// Put writes the provided key-value item into the data store.
// Returns an error if the item could not be written, nil otherwise.
func (db *BytesConnectionEtcd) Put(key string, binData []byte, opts ...keyval.PutOption) error {
	return putInternal(db.etcdClient, db.lessor, key, binData, opts...)
}

func putInternal(kv clientv3.KV, lessor clientv3.Lease, key string, binData []byte, opts ...keyval.PutOption) error {

	var etcdOpts []clientv3.OpOption
	for _, o := range opts {
		if withTTL, ok := o.(*keyval.WithTTLOpt); ok && withTTL.TTL > 0 {
			lease, err := lessor.Grant(context.Background(), int64(withTTL.TTL/time.Second))
			if err != nil {
				return err
			}
			etcdOpts = append(etcdOpts, clientv3.WithLease(lease.ID))
		}
	}

	_, err := kv.Put(context.Background(), key, string(binData), etcdOpts...)
	if err != nil {
		log.Error("etcdv3 put error: ", err)
		return err
	}
	return nil
}

// Delete removes data identified by the key.
func (db *BytesConnectionEtcd) Delete(key string) (bool, error) {
	return deleteInternal(db.etcdClient, key)
}

func deleteInternal(kv clientv3.KV, key string) (bool, error) {
	// delete data from etcdv3
	resp, err := kv.Delete(context.Background(), key)
	if err != nil {
		log.Error("etcdv3 error: ", err)
		return false, err
	}

	if len(resp.PrevKvs) == 0 {
		return true, nil
	}

	return false, nil
}

// GetValue retrieves one key-value item from the data store. The item
// is identified by the provided key.
func (db *BytesConnectionEtcd) GetValue(key string) (data []byte, found bool, revision int64, err error) {
	return getValueInternal(db.etcdClient, key)
}

func getValueInternal(kv clientv3.KV, key string) (data []byte, found bool, revision int64, err error) {
	// get data from etcdv3
	resp, err := kv.Get(context.Background(), key)
	if err != nil {
		log.Error("etcdv3 error: ", err)
		return nil, false, 0, err
	}

	for _, ev := range resp.Kvs {
		return ev.Value, true, ev.ModRevision, nil
	}
	return nil, false, 0, nil
}

// ListValues returns an iterator that enables to traverse values stored under the provided key.
func (db *BytesConnectionEtcd) ListValues(key string) (keyval.BytesKeyValIterator, error) {
	return listValuesInternal(db.etcdClient, key)
}

func listValuesInternal(kv clientv3.KV, key string) (keyval.BytesKeyValIterator, error) {
	// get data from etcdv3
	resp, err := kv.Get(context.Background(), key, clientv3.WithPrefix())
	if err != nil {
		log.Error("etcdv3 error: ", err)
		return nil, err
	}

	return &bytesKeyValIterator{len: len(resp.Kvs), resp: resp}, nil
}

// ListKeys is similar to the ListValues the difference is that values are not fetched
func (db *BytesConnectionEtcd) ListKeys(prefix string) (keyval.BytesKeyIterator, error) {
	return listKeysInternal(db.etcdClient, prefix)
}

func listKeysInternal(kv clientv3.KV, prefix string) (keyval.BytesKeyIterator, error) {
	// get data from etcdv3
	resp, err := kv.Get(context.Background(), prefix, clientv3.WithPrefix(), clientv3.WithKeysOnly())
	if err != nil {
		log.Error("etcdv3 error: ", err)
		return nil, err
	}

	return &bytesKeyIterator{len: len(resp.Kvs), resp: resp}, nil
}

// ListValuesRange returns an iterator that enables to traverse values stored under the provided key.
func (db *BytesConnectionEtcd) ListValuesRange(fromPrefix string, toPrefix string) (keyval.BytesKeyValIterator, error) {
	return listValuesRangeInternal(db.etcdClient, fromPrefix, toPrefix)
}

func listValuesRangeInternal(kv clientv3.KV, fromPrefix string, toPrefix string) (keyval.BytesKeyValIterator, error) {
	// get data from etcdv3
	resp, err := kv.Get(context.Background(), fromPrefix, clientv3.WithRange(toPrefix))
	if err != nil {
		log.Error("etcdv3 error: ", err)
		return nil, err
	}

	return &bytesKeyValIterator{len: len(resp.Kvs), resp: resp}, nil
}

// GetNext returns the following item from the result set. If data was returned found is set to true.
func (ctx *bytesKeyValIterator) GetNext() (val keyval.BytesKeyVal, lastReceived bool) {

	if ctx.index >= ctx.len {
		return nil, true
	}
	key := string(ctx.resp.Kvs[ctx.index].Key)
	data := ctx.resp.Kvs[ctx.index].Value
	rev := ctx.resp.Kvs[ctx.index].ModRevision
	ctx.index++
	return &bytesKeyVal{key, data, rev}, false
}

// GetNext returns the following item from the result set. If data was returned found is set to true.
func (ctx *bytesKeyIterator) GetNext() (key string, rev int64, lastReceived bool) {

	if ctx.index >= ctx.len {
		return "", 0, true
	}

	key = string(ctx.resp.Kvs[ctx.index].Key)
	rev = ctx.resp.Kvs[ctx.index].ModRevision
	ctx.index++
	return key, rev, false
}

// GetValue returns the value of the pair
func (kv *bytesKeyVal) GetValue() []byte {
	return kv.value
}

// GetKey returns the key of the pair
func (kv *bytesKeyVal) GetKey() string {
	return kv.key
}

// GetRevision returns the revision associated with the pair
func (kv *bytesKeyVal) GetRevision() int64 {
	return kv.revision
}