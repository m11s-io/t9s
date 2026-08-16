package tui

type contentSize struct {
	Width  int
	Height int
}

func resourceWindow(total, selected, capacity int) (int, int) {
	if total <= 0 || capacity <= 0 {
		return 0, 0
	}
	capacity = min(capacity, total)
	selected = min(max(selected, 0), total-1)
	start := max(0, selected-capacity+1)
	start = min(start, total-capacity)
	return start, start + capacity
}
