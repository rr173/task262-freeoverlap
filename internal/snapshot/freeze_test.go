package snapshot

import (
	"testing"

	"task262-freeoverlap/internal/model"
)

func TestCreateDraft(t *testing.T) {
	sn := Create("b1", "v1", "{}")
	if sn.Status != model.SnapshotDraft || sn.FrozenAt != 0 || sn.ID == "" {
		t.Fatalf("draft snapshot wrong: %+v", sn)
	}
}

func TestPublishOnce(t *testing.T) {
	sn := Create("b1", "v1", "{}")
	if err := Publish(sn); err != nil {
		t.Fatal(err)
	}
	if sn.Status != model.SnapshotPublished || sn.FrozenAt == 0 {
		t.Fatalf("publish failed: %+v", sn)
	}
	// 重复发布应报冲突。
	if err := Publish(sn); !model.IsKind(err, model.ErrConflict) {
		t.Fatalf("second publish should conflict, got %v", err)
	}
}

func TestSupersedeRequiresPublished(t *testing.T) {
	sn := Create("b1", "v1", "{}")
	if err := Supersede(sn); !model.IsKind(err, model.ErrStateMismatch) {
		t.Fatalf("supersede draft should fail, got %v", err)
	}
	if err := Publish(sn); err != nil {
		t.Fatal(err)
	}
	if err := Supersede(sn); err != nil {
		t.Fatalf("supersede published should work: %v", err)
	}
	if sn.Status != model.SnapshotSuperseded {
		t.Fatalf("status should be superseded: %+v", sn)
	}
}

func TestImmutableFlag(t *testing.T) {
	if !model.SnapshotPublished.IsImmutable() || !model.SnapshotSuperseded.IsImmutable() {
		t.Fatal("published/superseded should be immutable")
	}
	if model.SnapshotDraft.IsImmutable() {
		t.Fatal("draft should be mutable")
	}
}
