package utils_test

import (
	"bytes"
	"testing"

	"github.com/maddsua/proxyd/utils"
)

func TestNullStringReader_1(t *testing.T) {
	data := []byte{'t', 'e', 's', 't', 0, '\n', '2'}

	value, err := utils.ReadNullTerminatedString(bytes.NewReader(data), 100)
	if err != nil {
		t.Fatal("err:", err)
	} else if value != "test" {
		t.Fatalf("unexpected value: '%v'", value)
	}
}

func TestNullStringReader_2(t *testing.T) {
	data := []byte{'t', 'e', 's', 't', 0, '\n', '2'}

	value, err := utils.ReadNullTerminatedString(bytes.NewReader(data), 4)
	if err != nil {
		t.Fatal("err:", err)
	} else if value != "test" {
		t.Fatalf("unexpected value: '%v'", value)
	}
}

func TestNullStringReader_3(t *testing.T) {
	data := []byte{'t', 'e', 's', 't', 0, '\n', '2'}

	_, err := utils.ReadNullTerminatedString(bytes.NewReader(data), 2)
	if err == nil {
		t.Fatal("should've errorred but didn't")
	}
}
