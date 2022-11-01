package expiredmap

import (
	"encoding/json"
	"errors"
	"sync"
	"time"
)

// 定义 NodeManager 类型
type NodeManager struct {
	dict           sync.Map
	rootMapTracker *ExpiredMap[string, any]
	rootkey        string //根 关键字
	sync.RWMutex
}

// 创建新的 NodeManager 实例
func NewNodeManager() *NodeManager {
	return &NodeManager{}
	//不需要回调，回调不用回调自身
}

func (em *NodeManager) Get(key string) (value *ExpiredMap[string, any], ok bool) {
	var v any
	v, ok = em.dict.Load(key)
	if !ok {
		return
	}
	value, ok = v.(*ExpiredMap[string, any])

	return
}

func (em *NodeManager) Set(key string, value *ExpiredMap[string, any]) error {
	// 校验如果 value 为 *ExpiredMap 类型，则必须是 kind 为 0 的实例
	if value == nil {
		return errors.New("must insert is non nil ")
	}
	em.dict.Store(key, value)

	return nil
}

func (em *NodeManager) Delete(key string) error {
	em.dict.Delete(key)
	return nil
}

func (nm *NodeManager) InsertNode(rootKey string, ttl time.Duration) error {
	nm.Lock()
	defer nm.Unlock()
	if nm.rootMapTracker != nil {
		return errors.New("the node have exist")
	}

	em := NewRootMap[string, any](ttl)
	em.nodekey = rootKey
	nm.Set(rootKey, em)
	nm.rootMapTracker = em
	nm.rootkey = rootKey

	// 设置清理回调，当根节点的子节点被清理时调用
	em.SetOnExpire(func(key string, value any) {
		nm.handleChildExpire(key)
	})
	return nil
}
func (nm *NodeManager) InsertChild(parentKey, childKey string, ttl time.Duration) {
	//fmt.Println("lock", parentKey, childKey)
	nm.Lock()
	defer nm.Unlock()
	//defer fmt.Println("unlock", parentKey, childKey)

	// 从 nm.m 中查找父节点
	parentNode, parentExists := nm.Get(parentKey)
	if !parentExists {
		//log.Printf("Parent node %s does not exist.\n", parentKey)
		return
	}

	// 查找或创建子节点
	childNode, childExists := nm.Get(childKey)
	if !childExists {
		childNode = NewChildMap[string, any](ttl)
		nm.Set(childKey, childNode)
		parentNode.Set(childKey, childNode, ttl)
		childNode.SetOnExpire(func(key string, value any) {
			nm.handleChildExpire(key) // 处理子节点过期
		})
	} else {
		if parentNode.depth+1 < childNode.depth { //子节点已经存在，此时子节点depth 如果比要插入的父depth
			// 要深 ，说明叶子节点，可以进行操作，否则不应插入到新父点节下面
			childNode.SetParnet(parentNode) //通过这一步，以及上面的存在判断，防止出现拓扑环

		}
		if parentNode.nodekey == parentKey { //说明是原来的关系，需要更新超时时间
			parentNode.SetTTL(childKey, ttl)
		}
	}

}

func (nm *NodeManager) handleChildExpire(childKey string) {
	nm.Delete(childKey)

}

func (nm *NodeManager) Close() {
	nm.rootMapTracker.Close()

}

func (nm *NodeManager) MarshalJSON() ([]byte, error) {
	// 创建一个普通的 map 用于存储 JSON 序列化结果

	return json.Marshal(nm.rootMapTracker)
}

/* func main() {
	nodeManager := NewNodeManager(10 * time.Minute)

	// 示例用法
	rootNode := nodeManager.createNode("root", 5*time.Minute)
	nodeManager.insertChild("root", "child1", 5*time.Minute)
	nodeManager.insertChild("root", "child2", 5*time.Minute)
	nodeManager.insertChild("child1", "grandchild1", 5*time.Minute)

	// 清理过期的节点
	nodeManager.cleanupExpiredNodes()

	// 打印树结构
	rootNodeData, _ := nodeManager.m.Get("root")
	if rootNodeData != nil {
		rootNode := rootNodeData.(*ExpiredMap[string, any])
		fmt.Println("Root Node:")
		rootNode.HandleForeach(func(key string, value any) {
			fmt.Printf("  %s\n", key)
		})
	}

	// 关闭 nodeManager
	nodeManager.Close()
} */
