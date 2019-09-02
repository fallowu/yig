package meta

import (
	"context"

	. "github.com/journeymidnight/yig/error"
	"github.com/journeymidnight/yig/helper"
	. "github.com/journeymidnight/yig/meta/types"
	"github.com/journeymidnight/yig/redis"
)

func (m *Meta) GetCluster(ctx context.Context, fsid string, pool string) (cluster Cluster, err error) {
	rowKey := fsid + ObjectNameSeparator + pool
	getCluster := func() (c interface{}, err error) {
		helper.Logger.Println(10, "[", helper.RequestIdFromContext(ctx), "]", "GetCluster CacheMiss. fsid:", fsid)
		return m.Client.GetCluster(fsid, pool)
	}
	unmarshaller := func(in []byte) (interface{}, error) {
		var cluster Cluster
		err := helper.MsgPackUnMarshal(in, &cluster)
		return cluster, err
	}
	c, err := m.Cache.Get(ctx, redis.ClusterTable, rowKey, getCluster, unmarshaller, true)
	if err != nil {
		return
	}
	cluster, ok := c.(Cluster)
	if !ok {
		err = ErrInternalError
		return
	}
	return cluster, nil
}
