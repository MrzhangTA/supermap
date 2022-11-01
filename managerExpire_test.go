package expiredmap

import (
	"encoding/json"
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// 测试节点的创建和基础功能
func TestNodeManager_CreateAndInsertNode(t *testing.T) {
	nm := NewNodeManager()

	// 测试创建根节点
	err := nm.InsertNode("root", 5*time.Minute)
	assert.NoError(t, err, "创建根节点失败")

	// 测试创建重复的根节点
	err = nm.InsertNode("root", 5*time.Minute)
	assert.Error(t, err, "重复创建根节点应该报错")

	// 验证根节点是否存在
	root, ok := nm.Get("root")
	assert.True(t, ok, "根节点应该存在")
	assert.NotNil(t, root, "根节点不应该为空")
}

// 测试插入子节点
func TestNodeManager_InsertChild(t *testing.T) {
	nm := NewNodeManager()

	// 创建根节点
	err := nm.InsertNode("root", 5*time.Minute)
	assert.NoError(t, err, "创建根节点失败")

	// 插入子节点
	nm.InsertChild("root", "child1", 5*time.Minute)
	child, ok := nm.Get("child1")
	assert.True(t, ok, "子节点应该存在")
	assert.NotNil(t, child, "子节点不应该为空")

	// 插入嵌套子节点
	nm.InsertChild("child1", "grandchild1", 5*time.Minute)
	grandchild, ok := nm.Get("grandchild1")
	assert.True(t, ok, "孙节点应该存在")
	assert.NotNil(t, grandchild, "孙节点不应该为空")
}

// 测试过期删除
func TestNodeManager_NodeExpiration(t *testing.T) {
	nm := NewNodeManager()

	// 创建根节点和子节点
	err := nm.InsertNode("root", 1*time.Second)
	assert.NoError(t, err, "创建根节点失败")
	nm.InsertChild("root", "child1", 1*time.Second)

	// 等待节点过期
	time.Sleep(2 * time.Second)

	// 验证节点是否被删除
	_, ok := nm.Get("child1")
	assert.False(t, ok, "子节点应该过期并被删除")
}

// 测试防止环路
func TestNodeManager_CircularReference(t *testing.T) {
	nm := NewNodeManager()

	// 创建根节点和子节点
	err := nm.InsertNode("root", 5*time.Minute)
	assert.NoError(t, err, "创建根节点失败")
	nm.InsertChild("root", "child1", 5*time.Minute)

	// 插入一个使得环路形成的节点，应该被拒绝
	nm.InsertChild("child1", "root", 5*time.Minute)
	root, _ := nm.Get("root")
	assert.Equal(t, int64(1), root.Size(), "环路插入应该被拒绝，根节点应该只有一个子节点")
	child, _ := root.Get("child1")
	tt := child.(*ExpiredMap[string, any])
	assert.Equal(t, int64(0), tt.Size(), "环路插入应该被拒绝，根节点应该只有一个子节点")
}

// 测试深度判断
func TestNodeManager_DepthCheck(t *testing.T) {
	nm := NewNodeManager()

	// 插入根节点
	nm.InsertNode("root1", 10*time.Minute)
	root2 := nm.InsertNode("root2", 10*time.Minute)
	if root2 == nil {
		t.Fatal("Failed to insert root nodes.")
	}

	// 插入子节点到root1
	nm.InsertChild("root1", "child1", 5*time.Minute)
	nm.InsertChild("root1", "child2", 5*time.Minute)
	nm.InsertChild("root1", "child1", 5*time.Minute)
	root, _ := nm.Get("root1")
	assert.Equal(t, int64(2), root.Size(), "环路插入应该被拒绝，根节点应该只有一个子节点")
	// 插入孙节点到child1
	nm.InsertChild("child1", "grandchild1", 5*time.Minute)
	nm.InsertChild("grandchild1", "grandchild111", 5*time.Minute)
	nm.InsertChild("grandchild1", "grandchild122", 5*time.Minute)
	nm.InsertChild("child2", "grandchild2", 5*time.Minute)

	// 尝试将同一子节点插入到较浅的父节点，应该成功
	nm.InsertChild("root1", "grandchild1", 5*time.Minute)

	// 检查是否存在死循环（即：反复引用自身）
	// 尝试将child1插入到它的孙节点，应该被拒绝
	nm.InsertChild("grandchild1", "child1", 5*time.Minute)

	// 再次尝试将同一子节点插入到另一个根节点
	nm.InsertChild("root2", "child1", 5*time.Minute)
	data, _ := json.Marshal(root)
	fmt.Printf("data:%+v", string(data))
	assert.Equal(t, int64(3), root.Size(), "环路插入应该被拒绝，根节点应该只有一个子节点")

}

// 并发压测，测试嵌入、环状网络和深度切换的问题
func TestNodeManager_ConcurrentStressTest(t *testing.T) {
	go func() {
		http.ListenAndServe("localhost:6060", nil)
	}()
	nm := NewNodeManager()

	// 并发数量设置为一个较大的值，模拟高并发
	const numGoroutines = 1000
	const maxDepth = 10

	var wg sync.WaitGroup
	var mu sync.Mutex

	// 创建根节点
	err := nm.InsertNode("root", 5*time.Minute)
	assert.NoError(t, err, "创建根节点失败")

	// 并发插入和嵌套操作
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(i int) {
			defer wg.Done()

			mu.Lock()
			defer mu.Unlock()

			// 模拟大量嵌套插入操作
			parentNode := "root"
			childNode := "child" + strconv.Itoa(i)
			gradparent := "child" + strconv.Itoa(i-1)
			nm.InsertChild(parentNode, childNode, 5*time.Minute)

			// 模拟嵌套深度操作，递归嵌套直到最大深度
			for depth := 1; depth <= maxDepth; depth++ {
				if depth > 3 {
					gradparent = gradparent + "_sub" + strconv.Itoa(depth-3)
				}
				parentNode = childNode
				childNode = childNode + "_sub" + strconv.Itoa(depth)
				nm.InsertChild(parentNode, childNode, 5*time.Minute)
				if depth == maxDepth {
					fmt.Println("插入嵌入数据", gradparent, parentNode)
					nm.InsertChild(gradparent, parentNode, 5*time.Minute)
				}

			}

			// 测试子节点的深度切换问题
			newParentNode := "new_parent" + strconv.Itoa(i)
			err = nm.InsertNode(newParentNode, 5*time.Minute)
			assert.Error(t, err, "创建新的父节点失败")

		}(i)
	}

	// 等待所有并发操作完成
	wg.Wait()
	root, ok := nm.Get("root")
	assert.True(t, ok, "根节点应该存在")
	assert.NotNil(t, root, "根节点不应该为空")
	assert.Equal(t, int64(numGoroutines), root.Size(), "根节点的子节点数量应该等于并发数量")

	// 进一步验证每个节点的嵌套深度以及环状网络防护
	p, ok := nm.Get("child998_sub1_sub2_sub3_sub4_sub5_sub6_sub7")
	if ok {
		_, ok := p.Get("child999_sub1_sub2_sub3_sub4_sub5_sub6_sub7_sub8_sub9")
		assert.Equal(t, true, ok, "父切点应该切换到此节点")

	}
	p, ok = nm.Get("child989_sub1_sub2_sub3_sub4_sub5_sub6_sub7")
	if ok {
		_, ok := p.Get("child990_sub1_sub2_sub3_sub4_sub5_sub6_sub7_sub8_sub9")
		assert.True(t, ok, "父切点应该切换到此节点")

	}
}
