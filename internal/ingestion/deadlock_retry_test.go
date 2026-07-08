package ingestion

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestIsDeadlock(t *testing.T) {
	t.Run("true for 40P01", func(t *testing.T) {
		err := &pgconn.PgError{Code: "40P01"}
		if !isDeadlock(err) {
			t.Fatal("expected true for deadlock error")
		}
	})
	t.Run("false for other PgError", func(t *testing.T) {
		err := &pgconn.PgError{Code: "23505"}
		if isDeadlock(err) {
			t.Fatal("expected false for non-deadlock PgError")
		}
	})
	t.Run("false for non-PgError", func(t *testing.T) {
		if isDeadlock(errors.New("some error")) {
			t.Fatal("expected false for generic error")
		}
	})
	t.Run("false for nil", func(t *testing.T) {
		if isDeadlock(nil) {
			t.Fatal("expected false for nil")
		}
	})
}

func TestWithDeadlockRetry(t *testing.T) {
	t.Run("succeeds first try", func(t *testing.T) {
		calls := 0
		err := withDeadlockRetry("test", func() error {
			calls++
			return nil
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if calls != 1 {
			t.Fatalf("expected 1 call, got %d", calls)
		}
	})

	t.Run("retries on deadlock then succeeds", func(t *testing.T) {
		calls := 0
		err := withDeadlockRetry("test", func() error {
			calls++
			if calls < 3 {
				return &pgconn.PgError{Code: "40P01"}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if calls != 3 {
			t.Fatalf("expected 3 calls, got %d", calls)
		}
	})

	t.Run("returns non-deadlock error immediately", func(t *testing.T) {
		calls := 0
		want := errors.New("bad data")
		err := withDeadlockRetry("test", func() error {
			calls++
			return want
		})
		if !errors.Is(err, want) {
			t.Fatalf("expected %v, got %v", want, err)
		}
		if calls != 1 {
			t.Fatalf("expected 1 call, got %d", calls)
		}
	})

	t.Run("exhausts retries on persistent deadlock", func(t *testing.T) {
		calls := 0
		err := withDeadlockRetry("test", func() error {
			calls++
			return &pgconn.PgError{Code: "40P01"}
		})
		if err == nil {
			t.Fatal("expected error after exhausting retries")
		}
		if calls != deadlockRetryAttempts {
			t.Fatalf("expected %d calls, got %d", deadlockRetryAttempts, calls)
		}
	})
}

func TestSortDigestKeys(t *testing.T) {
	keys := []DigestKey{
		{OrgID: "org1", ClusterUUID: "c1", Namespace: "ns-b", Workload: "deploy-a", ContainerName: "ctr-a"},
		{OrgID: "org1", ClusterUUID: "c1", Namespace: "ns-a", Workload: "deploy-a", ContainerName: "ctr-a"},
		{OrgID: "org1", ClusterUUID: "c1", Namespace: "ns-a", Workload: "deploy-a", ContainerName: "ctr-b"},
	}
	sortDigestKeys(keys)
	if keys[0].Namespace != "ns-a" || keys[0].ContainerName != "ctr-a" {
		t.Fatalf("first key should be ns-a/ctr-a, got %s/%s", keys[0].Namespace, keys[0].ContainerName)
	}
	if keys[1].Namespace != "ns-a" || keys[1].ContainerName != "ctr-b" {
		t.Fatalf("second key should be ns-a/ctr-b, got %s/%s", keys[1].Namespace, keys[1].ContainerName)
	}
	if keys[2].Namespace != "ns-b" {
		t.Fatalf("third key should be ns-b, got %s", keys[2].Namespace)
	}
}
