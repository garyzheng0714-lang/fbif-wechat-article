package analytics

import (
	"context"
	"time"
)

// RanSince 说 since 之后是不是每个现役接口都成功拉过：是，就说明这一轮已经跑完，
// 重启时不用再补跑一整轮；有一个没成功（失败、额度不够、还没跑），就照旧补跑。
func (c *Collector) RanSince(ctx context.Context, since time.Time) (bool, error) {
	for _, endpoint := range c.activeEndpoints() {
		state, err := c.Store.GetState(ctx, endpoint.Name)
		if err != nil {
			return false, err
		}
		if state == nil || state.LastSuccessAt < since.UnixMilli() {
			return false, nil
		}
	}
	return true, nil
}
