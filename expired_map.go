package expiredmap

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"errors"

	"go.uber.org/atomic"
)

type ExpiredMap[TK comparable, TV any] struct {
	m              sync.Map
	expireInterval time.Duration
	len            atomic.Int64
	stop           chan bool
	child          bool
	onExpire       func(key TK, value TV)
	Parent         *ExpiredMap[TK, TV]
	depth          int
	nodekey        TK
}

type expiredMapItem[TK comparable, TV any] struct {
	key      TK
	value    TV
	expireAt atomic.Value
}

func (em *ExpiredMap[TK, TV]) start() {
	if em.child {
		return
	}

	go func() {
		ticker := time.NewTicker(em.expireInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				em.cleanup()
			case <-em.stop:
				return
			}
		}
	}()
}

func (em *ExpiredMap[TK, TV]) SetOnExpire(callback func(key TK, value TV)) {
	em.onExpire = callback
}

func (em *ExpiredMap[TK, TV]) cleanup() {
	em.m.Range(func(key, value any) bool {
		if v, ok := value.(*expiredMapItem[TK, TV]); ok {

			if time.Now().After(v.expireAt.Load().(time.Time)) {
				em.Delete(key.(TK)) //普通value 不需要回调
			}

		}
		return true
	})
}

func (em *ExpiredMap[TK, TV]) Set(key TK, value TV, ttl time.Duration) error {
	// 校验如果 value 为 *ExpiredMap 类型，则必须是 kind 为 0 的实例
	if nestedMap, ok := any(value).(JSONSerializable); ok {
		if !nestedMap.IsExpiredMap() || !nestedMap.IsChild() {
			return errors.New("can't insert root expire map")
		}
	}

	if serializable, ok := any(value).(*ExpiredMap[TK, TV]); ok && serializable.child {
		serializable.Parent = em
		serializable.depth = em.depth + 1
		serializable.nodekey = key
	}
	// 创建新的 expiredMapItem
	newItem := &expiredMapItem[TK, TV]{
		key:   key,
		value: value,
	}
	newItem.expireAt.Store(time.Now().Add(ttl))
	// 插入 item
	em.m.Store(key, newItem)
	em.len.Inc()
	return nil
}

func (em *ExpiredMap[TK, TV]) Get(key TK) (value TV, ok bool) {
	var v any
	v, ok = em.m.Load(key)
	if !ok {
		return
	}

	v2, ok2 := v.(*expiredMapItem[TK, TV])
	if !ok2 || v2 == nil {
		ok = false
		return
	}

	if time.Now().After(v2.expireAt.Load().(time.Time)) {
		ok = false
		em.Delete(key)
		return
	}
	value = v2.value
	return
}

func (em *ExpiredMap[TK, TV]) Delete(key TK) {
	value, ok := em.m.Load(key)
	if !ok {
		return
	}

	// 如果是嵌套的 ExpiredMap 类型，需要判断是否是子节点
	if nestedMap, ok := value.(*expiredMapItem[TK, TV]); ok {
		if serializable, ok := any(nestedMap.value).(*ExpiredMap[TK, TV]); ok && serializable.child {
			// 调用外部的 NodeManager 来减少子节点的引用计数
			serializable.HandleForeachDelete() //需要先删除所有的子节点，才能删除它本身

		}
		if em.onExpire != nil {
			em.onExpire(key, nestedMap.value)
		}
	} else {
		//panic("have 致命错误")
	}

	// 如果是普通的键值对或没有嵌套的 ExpiredMap，直接删除
	em.m.Delete(key)
	em.len.Dec()
}

func (em *ExpiredMap[TK, TV]) SetParnet(newParent *ExpiredMap[TK, TV]) {
	if em.Parent != nil {
		em.Parent.m.Delete(em.nodekey)
		em.Parent.len.Dec()
	}
	if newParent != nil {
		newParent.m.Store(em.nodekey, em)
		newParent.len.Inc()
	}
	em.Parent = newParent
	em.depth = newParent.depth + 1
	em.HandleForeachDepth() //修改子节点深度

}

func (em *ExpiredMap[TK, TV]) Size() int64 {
	return em.len.Load()
}
func (em *ExpiredMap[TK, TV]) IsChild() bool {
	return em.child
}

func (em *ExpiredMap[TK, TV]) IsExpiredMap() bool {
	return true
}

func (em *ExpiredMap[TK, TV]) TTL(key TK) time.Duration {
	v, ok := em.m.Load(key)
	if !ok {
		return -2
	}
	v2, ok2 := v.(*expiredMapItem[TK, TV])
	if !ok2 || v2 == nil {
		return -2
	}
	now := time.Now()
	if now.After(v2.expireAt.Load().(time.Time)) {
		em.Delete(key)
		return -1
	}
	return v2.expireAt.Load().(time.Time).Sub(now)
}

func (em *ExpiredMap[TK, TV]) SetTTL(key TK, ttl time.Duration) int {
	v, ok := em.m.Load(key)
	if !ok {
		return -2
	}
	v2, ok2 := v.(*expiredMapItem[TK, TV])
	if !ok2 || v2 == nil {
		return -2
	}
	v2.expireAt.Store(time.Now().Add(ttl))
	// 插入 item
	em.m.Store(key, v2)
	return 1
}

func (em *ExpiredMap[TK, TV]) HandleForeachDelete() {
	em.m.Range(func(key, value any) bool {
		v, ok := value.(*expiredMapItem[TK, TV])
		if !ok || v == nil {
			return false
		}
		if serializable, ok := any(v.value).(*ExpiredMap[TK, TV]); ok && serializable.child {
			serializable.HandleForeachDelete()
			if em.onExpire != nil {
				TKey := key.(TK)
				em.onExpire(TKey, v.value)
			}
		}

		em.Delete(key.(TK))
		return true
	})
}

func (em *ExpiredMap[TK, TV]) HandleForeachDepth() {
	em.m.Range(func(key, value any) bool {
		v, ok := value.(*expiredMapItem[TK, TV])
		if !ok || v == nil {
			return true
		}
		if serializable, ok := any(v.value).(*ExpiredMap[TK, TV]); ok && serializable.child {
			serializable.depth = em.depth + 1
			serializable.HandleForeachDepth()
		}

		return true
	})
}

func (em *ExpiredMap[TK, TV]) HandleForeach(callback func(key TK, value TV)) {
	em.m.Range(func(key, value any) bool {
		v, ok := value.(*expiredMapItem[TK, TV])
		if !ok || v == nil {
			return false
		}
		if time.Now().After(v.expireAt.Load().(time.Time)) {
			TKey := key.(TK)
			em.Delete(TKey)
			return true
		}
		TKey := key.(TK)
		callback(TKey, v.value)
		return true
	})
}

func (em *ExpiredMap[TK, TV]) Close() {
	em.stop <- true
	em.m.Range(func(key, value any) bool {
		TKey := key.(TK)
		em.Delete(TKey)
		return true
	})
}

func NewRootMap[TK comparable, TV any](expireInterval time.Duration) *ExpiredMap[TK, TV] {
	em := &ExpiredMap[TK, TV]{
		expireInterval: expireInterval,
		child:          false,
		stop:           make(chan bool),
	}
	em.start() // 启动清理协程
	return em
}

// NewChildMap 创建一个新的 kind 为 0 的子级 ExpiredMap 实例
func NewChildMap[TK comparable, TV any](expireInterval time.Duration) *ExpiredMap[TK, TV] {
	return &ExpiredMap[TK, TV]{
		expireInterval: expireInterval,
		child:          true, // kind 为 0
	}
}

type JSONSerializable interface {
	MarshalJSON() ([]byte, error)
	IsExpiredMap() bool
	IsChild() bool
}

/* func (em *ExpiredMap[TK, TV]) MarshalJSON() ([]byte, error) {
	// 创建一个普通的 map 用于存储 JSON 序列化结果
	normalMap := make(map[string]any)
	// 遍历 sync.Map
	em.m.Range(func(key, value interface{}) bool {
		if k, ok := key.(TK); ok {
			if v, ok := value.(*expiredMapItem[TK, TV]); ok {
				// 处理值的类型
				if serializable, ok := any(v.value).(*ExpiredMap[TK, TV]); ok && v != nil {
					//if serializable, ok := any(v.value).(*ExpiredMap[TK, TV]); ok {   //两种写法都是可以的
					if serializable != nil {
						data, err := serializable.MarshalJSON()
						if err != nil {
							return false
						}

						var nestedMapResult map[string]any
						if err := json.Unmarshal(data, &nestedMapResult); err != nil {
							return false
						}
						normalMap[formatKey(k)] = nestedMapResult

					}

				} else {
					// 直接序列化普通值
					normalMap[formatKey(k)] = v.value
				}
			} else {
				// 如果值不是 expiredMapItem，直接存储为值
				normalMap[formatKey(k)] = value
			}
		}
		return true
	})

	return json.Marshal(normalMap)
} */

type Node struct {
	IP       string      `json:"ip"`
	Children interface{} `json:"children"`
}

func (em *ExpiredMap[TK, TV]) MarshalJSON() ([]byte, error) {
	// 创建一个普通的 map 用于存储 JSON 序列化结果
	var normalMap []*Node
	// 遍历 sync.Map
	em.m.Range(func(key, value interface{}) bool {
		if k, ok := key.(TK); ok {
			if v, ok := value.(*expiredMapItem[TK, TV]); ok {
				// 处理值的类型
				if serializable, ok := any(v.value).(*ExpiredMap[TK, TV]); ok && v != nil {
					//if serializable, ok := any(v.value).(*ExpiredMap[TK, TV]); ok {   //两种写法都是可以的
					if serializable != nil {
						data, err := serializable.MarshalJSON()
						if err != nil {
							return false
						}

						var nestedMapResult []*Node
						if err := json.Unmarshal(data, &nestedMapResult); err != nil {
							return false
						}
						var node Node
						node.IP = formatKey(k)
						node.Children = nestedMapResult
						normalMap = append(normalMap, &node)

					}

				} else {
					// 直接序列化普通值
					var node Node
					node.IP = formatKey(k)
					node.Children = value
					normalMap = append(normalMap, &node)
				}
			} else {
				// 如果值不是 expiredMapItem，直接存储为值
				var node Node
				node.IP = formatKey(k)
				node.Children = value
				normalMap = append(normalMap, &node)
			}
		}
		return true
	})

	return json.Marshal(normalMap)
}

func formatKey[TK comparable](key TK) string {
	// 假设键可以直接转为字符串，如果不行需要进行自定义处理
	return fmt.Sprintf("%v", key)
}

func (em *ExpiredMap[TK, TV]) UnmarshalJSON(data []byte) error {
	// 解析 JSON 数据到一个通用的 map[string]any 类型
	var rawMap map[string]any
	if err := json.Unmarshal(data, &rawMap); err != nil {
		return err
	}
	// 处理 map 中的每个键值对
	for k, v := range rawMap {
		// 处理嵌套的 ExpiredMap
		if nestedMapJSON, ok := v.(map[string]any); ok {
			// 创建一个新的 ExpiredMap 实例
			nestedMap := &ExpiredMap[TK, TV]{
				expireInterval: em.expireInterval, // 使用相同的过期时间      // 使用相同的容量
				child:          true,              // 嵌套的 kind 为 0
			}

			// 递归反序列化嵌套的 ExpiredMap
			nestedMapData, err := json.Marshal(nestedMapJSON)
			if err != nil {
				return err
			}
			if err := nestedMap.UnmarshalJSON(nestedMapData); err != nil {
				return err
			}
			expireAt := atomic.Value{}
			expireAt.Store(time.Now().Add(5 * em.expireInterval))
			item := &expiredMapItem[TK, TV]{
				key:      any(k).(TK),
				value:    any(nestedMap).(TV),
				expireAt: expireAt,
			}

			// 存储嵌套的 ExpiredMap 实例
			em.m.Store(k, item)
		} else {
			// 处理普通值
			var value TV
			valueData, err := json.Marshal(v)
			if err != nil {
				return err
			}
			if err := json.Unmarshal(valueData, &value); err != nil {
				return err
			}
			expireAt := atomic.Value{}
			expireAt.Store(time.Now().Add(5 * em.expireInterval))

			item := &expiredMapItem[TK, TV]{
				key:      any(k).(TK),
				value:    value,
				expireAt: expireAt,
			}
			em.m.Store(k, item)
		}
	}
	return nil
}

/*

// type CleanupCallback[TK, TV any] func(nestedMap *ExpiredMap[TK, TV]) //你看到TK does not implement comparable的TK被用map的comparable。
type CleanupCallback1[TK comparable, TV any] func(nestedMap *ExpiredMap[TK, TV])

func (em *ExpiredMap[TK, TV]) cleanup1(callback CleanupCallback1[TK, TV]) {
	em.m.Range(func(key, value any) bool {
		if v, ok := value.(*expiredMapItem[TK, TV]); ok {
			if time.Now().After(v.expireAt.Load().(time.Time)) {
				em.Delete(key.(TK))
			}
		}

		if nestedMap, ok := value.(*ExpiredMap[TK, TV]); ok && nestedMap.child {
			if callback != nil {
				callback(nestedMap)
			}
			nestedMap.cleanup1(callback)
		}

		return true
	})
}

func myCallback[TK comparable, TV any](nestedMap *ExpiredMap[TK, TV]) {
	fmt.Println("准备清理嵌套的 ExpiredMap:", nestedMap)
}
func test11() {
	em := &ExpiredMap[string, int]{}
	callback := myCallback[string, int]

	//em.cleanup1(myCallback)   //这种用法会报错 未实例化时无法使用通用函数 myCallback
	em.cleanup1(callback)
}

*/

/*
type CleanupCallback[TK, TV any] func(key TK, value TV)
func (em *ExpiredMap[TK, TV]) cleanup2(callback CleanupCallback[TK, TV]) {
	em.m.Range(func(key, value any) bool {
		// 如果 value 是 expiredMapItem 类型
		if v, ok := value.(*expiredMapItem[TK, TV]); ok {
			// 如果当前时间已经过期
			if time.Now().After(v.expireAt.Load().(time.Time)) {
				// 调用回调函数，传递 key 和实际的 value
				callback(key.(TK), v.value)
				// 删除过期项
				em.m.Delete(key.(TK))
			}
		}

		// 如果 value 是另一个 ExpiredMap，需要递归处理
		if nestedMap, ok := value.(*ExpiredMap[TK, TV]); ok && nestedMap.child {
			// 递归调用 cleanup 处理过期，并传递回调函数
			nestedMap.cleanup2(callback)
		}

		return true
	})
}
func main2() {
	callback := func(key string, value any) {
		fmt.Printf("Expired key-value pair: Key = %s, Value = %d\n", key, value)
	}

	// 调用 cleanup 方法，并传入回调函数
	em.cleanup2(callback)
}
*/
