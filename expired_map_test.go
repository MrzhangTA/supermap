package expiredmap

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestExpiredMap_SetAndGet(t *testing.T) {
	em := NewRootMap[string, string](time.Second)

	err := em.Set("key1", "value1", time.Second*2)
	assert.NoError(t, err, "Set should not return an error")

	value, ok := em.Get("key1")
	assert.True(t, ok, "key1 should exist")
	assert.Equal(t, "value1", value, "value should match")
	time.Sleep(time.Second * 3)
	value, ok = em.Get("key1")
	assert.False(t, ok, "key1 not exist")
	assert.Equal(t, "", value, "value should match")
}

func TestExpiredMap_Expiration(t *testing.T) {
	em := NewRootMap[string, string](time.Millisecond * 500)

	err := em.Set("key1", "value1", time.Millisecond*100)
	assert.NoError(t, err, "Set should not return an error")

	time.Sleep(time.Millisecond * 200)

	value, ok := em.Get("key1")
	assert.False(t, ok, "key1 should be expired")
	assert.Equal(t, "", value, "expired key should return zero value")

	// Size should be 0 after expiration
	assert.Equal(t, int64(0), em.Size(), "Size should be 0 after expiration")
}

func TestExpiredMap_Delete(t *testing.T) {
	em := NewRootMap[string, string](time.Second)

	err := em.Set("key1", "value1", time.Second*2)
	assert.NoError(t, err, "Set should not return an error")

	em.Delete("key1")

	value, ok := em.Get("key1")
	assert.False(t, ok, "key1 should be deleted")
	assert.Equal(t, "", value, "deleted key should return zero value")
}

func TestExpiredMap_Cleanup(t *testing.T) {
	em := NewRootMap[string, string](time.Millisecond * 200)

	err := em.Set("key1", "value1", time.Millisecond*100)
	assert.NoError(t, err, "Set should not return an error")

	time.Sleep(time.Millisecond * 150)

	em.cleanup()

	value, ok := em.Get("key1")
	assert.False(t, ok, "key1 should be expired and cleaned up")
	assert.Equal(t, "", value, "expired and cleaned up key should return zero value")
}

func TestExpiredMap_MarshalJSON(t *testing.T) {
	em := NewRootMap[string, any](time.Second)

	nestedMap := NewChildMap[string, any](time.Second * 2)
	nestedMap.Set("185.123.68.4", "nestedValue1", time.Second*2)

	nestedMap2 := NewChildMap[string, any](time.Second * 2)
	nestedMap2.Set("172.163.241", "value2", time.Second*2)
	nestedMap.Set("185.123.68.4", nestedMap2, time.Second*2)

	nestedMap.Set("185.123.68.6", "localhost", time.Second*2)
	em.Set("127.0.0.1", nestedMap, time.Second*2)
	em.Set("172.163.241", "value2", time.Second*2)

	em.Set("127.0.0.1", nestedMap, time.Second*2)
	em.Set("172.163.241", nestedMap, time.Second*2)

	jsonData, err := json.Marshal(em)

	fmt.Printf("jsonData =%+v\n", string(jsonData))

	assert.NoError(t, err, "MarshalJSON should not return an error")
}

func TestExpiredMap_TTL(t *testing.T) {
	em := NewRootMap[string, string](time.Second)

	err := em.Set("key1", "value1", time.Second*2)
	assert.NoError(t, err, "Set should not return an error")

	ttl := em.TTL("key1")
	assert.True(t, ttl > 0, "TTL should be greater than zero")

	time.Sleep(time.Second * 3)

	ttl = em.TTL("key1")
	assert.Greater(t, time.Duration(0), ttl, "TTL should be -1 for expired key")
}

func TestExpiredMap_UnmarshalJSON(t *testing.T) {
	jsonData := `{"key1":{"nestedKey1":"nestedValue1"},"key2":"value2"}`

	em := NewRootMap[string, any](time.Second)

	err := json.Unmarshal([]byte(jsonData), &em)
	assert.NoError(t, err, "UnmarshalJSON should not return an error")
	value1, ok := em.Get("key2")
	fmt.Printf("em=%+v,%v\n", value1, ok)
	assert.True(t, ok, "key2 should exist after unmarshalling")
	assert.Equal(t, "value2", value1, "value2 should match expected value")

	nestedMap, ok := em.Get("key1")
	assert.True(t, ok, "key1 should exist after unmarshalling")

	if nested, ok := nestedMap.(*ExpiredMap[string, any]); ok {
		nestedValue, ok := nested.Get("nestedKey1")
		assert.True(t, ok, "nestedKey1 should exist after unmarshalling")
		assert.Equal(t, "nestedValue1", nestedValue, "nestedValue1 should match expected value")
	} else {
		t.Error("key1 should be a nested ExpiredMap")
	}
}
