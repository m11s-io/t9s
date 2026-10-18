package tui

// listControl tracks selection independently of the rows a screen renders.
// Its zero value is an empty list with the cursor at zero.
type listControl struct {
	count  int
	cursor int
}

func (m listControl) Cursor() int { return m.cursor }

func (m *listControl) SetCount(count int) {
	m.count = max(0, count)
	m.SetCursor(m.cursor)
}

func (m *listControl) SetCursor(cursor int) {
	m.cursor = min(max(cursor, 0), max(0, m.count-1))
}

func (m *listControl) MoveUp(count int)   { m.SetCursor(m.cursor - count) }
func (m *listControl) MoveDown(count int) { m.SetCursor(m.cursor + count) }
func (m *listControl) GotoTop()           { m.SetCursor(0) }
func (m *listControl) GotoBottom()        { m.SetCursor(m.count - 1) }
