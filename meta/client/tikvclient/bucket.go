package tikvclient

import (
	"context"
	. "database/sql/driver"
	"math"
	"strings"

	"github.com/journeymidnight/yig/api/datatype"
	. "github.com/journeymidnight/yig/error"
	"github.com/journeymidnight/yig/helper"
	. "github.com/journeymidnight/yig/meta/types"
	"github.com/tikv/client-go/key"
)

func genBucketKey(bucketName string) []byte {
	return GenKey(TableBucketPrefix, bucketName)
}

//bucket
func (c *TiKVClient) GetBucket(bucketName string) (*Bucket, error) {
	key := genBucketKey(bucketName)
	var b Bucket
	ok, err := c.Get(key, &b)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrNoSuchBucket
	}
	return &b, nil
}

// TODO: To be deprecated
func (c *TiKVClient) GetBuckets() (buckets []Bucket, err error) {
	startKey := GenKey(TableBucketPrefix, TableMinKeySuffix)
	endKey := GenKey(TableBucketPrefix, TableMaxKeySuffix)
	kvs, err := c.Scan(startKey, endKey, math.MaxUint64)
	for _, kv := range kvs {
		var b Bucket
		err = helper.MsgPackUnMarshal(kv.V, &b)
		if err != nil {
			return nil, err
		}
		buckets = append(buckets, b)
	}
	return buckets, nil
}

func (c *TiKVClient) PutBucket(bucket Bucket) error {
	key := genBucketKey(bucket.Name)
	return c.Put(key, bucket)
}

func (c *TiKVClient) PutNewBucket(bucket Bucket) error {
	bucketKey := genBucketKey(bucket.Name)
	userBucketKey := genUserBucketKey(bucket.OwnerId, bucket.Name)
	return c.TxPut(bucketKey, bucketKey, userBucketKey, 0)
}

func (c *TiKVClient) DeleteBucket(bucket Bucket) error {
	bucketKey := genBucketKey(bucket.Name)
	userBucketKey := genUserBucketKey(bucket.OwnerId, bucket.Name)
	lifeCycleKey := genLifecycleKey()
	return c.TxDelete(bucketKey, userBucketKey, lifeCycleKey)
}

func (c *TiKVClient) ListObjects(bucketName, marker, prefix, delimiter string, maxKeys int) (info ListObjectsInfo, err error) {
	startKey := genObjectKey(bucketName, marker, NullVersion)
	endKey := genObjectKey(bucketName, TableMaxKeySuffix, NullVersion)

	tx, err := c.txnCli.Begin(context.TODO())
	if err != nil {
		return info, err
	}
	it, err := tx.Iter(context.TODO(), key.Key(startKey), key.Key(endKey))
	if err != nil {
		return info, err
	}
	defer it.Close()
	var commonPrefixes []string

	count := 0
	lastKey := ""
	for it.Valid() {
		k, v := string(it.Key()[:]), it.Value()
		if k == string(startKey) {
			continue
		}
		if !strings.HasPrefix(k, prefix) {
			continue
		}
		if delimiter != "" {
			subKey := strings.TrimPrefix(k, prefix)
			sp := strings.Split(subKey, delimiter)
			if len(sp) > 2 {
				continue
			} else if len(sp) == 2 {
				if sp[1] == "" {
					commonPrefixes = append(commonPrefixes, subKey)
					lastKey = k
					count++
					continue
				} else {
					continue
				}
			}
		}
		var o Object
		var info_o datatype.Object
		err = helper.MsgPackUnMarshal(v, &o)
		if err != nil {
			return info, err
		}
		info_o.Key = o.Name
		info_o.Owner = datatype.Owner{ID: o.OwnerId}
		info_o.ETag = o.Etag
		info_o.LastModified = o.LastModifiedTime.UTC().Format(CREATE_TIME_LAYOUT)
		info_o.Size = uint64(o.Size)
		info_o.StorageClass = o.StorageClass.ToString()
		lastKey = k
		count++
		info.Objects = append(info.Objects, info_o)

		if count == maxKeys {
			break
		}
		it.Next(context.TODO())
	}
	info.Prefixes = commonPrefixes
	it.Next(context.TODO())
	if it.Valid() {
		info.NextMarker = lastKey
		info.IsTruncated = true
	}
	return
}

func (c *TiKVClient) ListVersionedObjects(bucketName, marker, verIdMarker, prefix, delimiter string,
	maxKeys int) (info VersionedListObjectsInfo, err error) {

	return
}

func (c *TiKVClient) UpdateUsage(bucketName string, size int64, tx Tx) error {
	if !helper.CONFIG.PiggybackUpdateUsage {
		return nil
	}

	bucket, err := c.GetBucket(bucketName)
	if err != nil {
		return err
	}

	userBucketKey := genUserBucketKey(bucket.OwnerId, bucket.Name)
	var usage int64

	if tx == nil {
		ok, err := c.Get(userBucketKey, &usage)
		if err != nil {
			return err
		}
		if !ok {
			return ErrNoSuchBucket
		}
		usage += size
		return c.Put(userBucketKey, usage)
	}

	v, err := tx.(*TikvTx).tx.Get(context.TODO(), userBucketKey)
	if err != nil {
		return err
	}

	err = helper.MsgPackUnMarshal(v, &usage)
	if err != nil {
		return err
	}

	usage += size

	v, err = helper.MsgPackMarshal(usage)
	if err != nil {
		return err
	}
	return tx.(*TikvTx).tx.Set(userBucketKey, v)
}

func (c *TiKVClient) IsEmptyBucket(bucketName string) (isEmpty bool, err error) {
	bucketStartKey := GenKey(bucketName, TableMinKeySuffix)
	bucketEndKey := GenKey(bucketName, TableMaxKeySuffix)
	partStartKey := GenKey(TableObjectPartPrefix, bucketName, TableMinKeySuffix)
	partEndKey := GenKey(TableObjectPartPrefix, bucketName, TableMaxKeySuffix)
	r, err := c.Scan(bucketStartKey, bucketEndKey, 1)
	if err != nil {
		return false, err
	}
	if len(r) > 0 {
		return false, nil
	}
	r, err = c.Scan(partStartKey, partEndKey, 1)
	if err != nil {
		return false, err
	}
	if len(r) > 0 {
		return false, nil
	}
	return true, nil

}
