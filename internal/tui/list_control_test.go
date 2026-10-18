package tui

import "testing"

func TestListControlNavigationClampsToAvailableRows(t *testing.T) {
	var control listControl
	control.SetCount(4)
	control.MoveDown(2)
	if got := control.Cursor(); got != 2 {
		t.Fatalf("move down: got %d, want 2", got)
	}
	control.MoveDown(20)
	if got := control.Cursor(); got != 3 {
		t.Fatalf("past bottom: got %d, want 3", got)
	}
	control.MoveUp(2)
	if got := control.Cursor(); got != 1 {
		t.Fatalf("move up: got %d, want 1", got)
	}
	control.MoveUp(20)
	if got := control.Cursor(); got != 0 {
		t.Fatalf("past top: got %d, want 0", got)
	}
	control.GotoBottom()
	if got := control.Cursor(); got != 3 {
		t.Fatalf("bottom: got %d, want 3", got)
	}
	control.GotoTop()
	if got := control.Cursor(); got != 0 {
		t.Fatalf("top: got %d, want 0", got)
	}
}

func TestListControlRefreshKeepsSelectionWithinRows(t *testing.T) {
	var control listControl
	control.SetCount(5)
	control.SetCursor(3)
	control.SetCount(8)
	if got := control.Cursor(); got != 3 {
		t.Fatalf("growing list moved selection to %d", got)
	}
	control.SetCount(2)
	if got := control.Cursor(); got != 1 {
		t.Fatalf("shrinking list: got %d, want 1", got)
	}
	control.SetCursor(-10)
	if got := control.Cursor(); got != 0 {
		t.Fatalf("negative selection: got %d, want 0", got)
	}
	control.SetCursor(10)
	if got := control.Cursor(); got != 1 {
		t.Fatalf("oversized selection: got %d, want 1", got)
	}
	control.SetCount(0)
	control.MoveDown(1)
	control.MoveUp(1)
	control.GotoBottom()
	if got := control.Cursor(); got != 0 {
		t.Fatalf("empty list: got %d, want 0", got)
	}
	control.SetCount(3)
	if got := control.Cursor(); got != 0 {
		t.Fatalf("repopulated list: got %d, want 0", got)
	}
}
