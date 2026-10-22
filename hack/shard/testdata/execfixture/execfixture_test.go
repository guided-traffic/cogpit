package execfixture

import (
	"os"
	"testing"
)

func check(t *testing.T) {
	if os.Getenv("SHARD_FIXTURE_FAIL") == t.Name() {
		t.Fatal("failed on purpose")
	}
	if _, err := os.Stat("execfixture.go"); err != nil {
		t.Fatalf("not run in the package's directory: %v", err)
	}
}

func TestA(t *testing.T) { check(t) }
func TestB(t *testing.T) { check(t) }
func TestC(t *testing.T) { check(t) }
func TestD(t *testing.T) { check(t) }
func TestE(t *testing.T) { check(t) }
func TestF(t *testing.T) { check(t) }
