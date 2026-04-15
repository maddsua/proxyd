package utils_test

import (
	"testing"
	"time"

	"github.com/maddsua/proxyd/utils"
)

func Test_ExpireMap_1(t *testing.T) {

	var em utils.ExpireMap[int]
	em.Set("test", 42, 100*time.Millisecond)

	if val := em.Get("test"); val == nil || em.Get("test").Val != 42 {
		t.Fatal("invalid val:", val)
	}

	time.Sleep(150 * time.Millisecond)

	if val := em.Get("test"); val != nil {
		t.Fatal("invalid val:", val)
	}
}

func Test_ExpireMap_2(t *testing.T) {

	var em utils.ExpireMap[int]

	em.Set("test", 42, time.Minute)

	val := em.SetNoExist("test", 69, time.Minute)
	if val.Val != 42 {
		t.Fatal("unexpected overwrite:", val)
	}

	time.Sleep(2 * time.Second)

	if val = em.SetNoExist("test", 69, time.Minute); val.Val != 42 {
		t.Fatal("unexpected overwrite:", val)
	}
}

func Test_ExpireMap_3(t *testing.T) {

	var em utils.ExpireMap[int]

	em.Set("test", 42, time.Second)

	val := em.SetNoExist("test", 69, time.Second)
	if val.Val != 42 {
		t.Fatal("unexpected overwrite:", val)
	}

	time.Sleep(2 * time.Second)

	if val = em.SetNoExist("test", 69, time.Minute); val.Val != 69 {
		t.Fatal("unexpected presistence:", val)
	}
}
