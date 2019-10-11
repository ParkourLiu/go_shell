package main

import (
	"github.com/lxn/walk"
	"sort"
)

type Condom struct {
	IP      string
	Name    string
	Whoami  string
	Remark  string
	Time    string
	checked bool
}

type CondomModel struct {
	walk.TableModelBase
	walk.SorterBase
	sortColumn int
	sortOrder  walk.SortOrder
	items      []*Condom
}

func (m *CondomModel) RowCount() int {
	return len(m.items)
}

func (m *CondomModel) Value(row, col int) interface{} {
	item := m.items[row]

	switch col {
	case 0:
		return item.IP
	case 1:
		return item.Remark
	case 2:
		return item.Whoami
	case 3:
		return item.Name
	case 4:
		return item.Time
	}
	panic("unexpected col")
}

func (m *CondomModel) Checked(row int) bool {
	return m.items[row].checked
}

func (m *CondomModel) SetChecked(row int, checked bool) error {
	m.items[row].checked = checked
	return nil
}

func (m *CondomModel) Sort(col int, order walk.SortOrder) error {
	m.sortColumn, m.sortOrder = col, order

	sort.Stable(m)

	return m.SorterBase.Sort(col, order)
}

func (m *CondomModel) Len() int {
	return len(m.items)
}

func (m *CondomModel) Less(i, j int) bool {
	a, b := m.items[i], m.items[j]

	c := func(ls bool) bool {
		if m.sortOrder == walk.SortAscending {
			return ls
		}

		return !ls
	}

	switch m.sortColumn {
	case 0:
		return c(a.IP < b.IP)
	case 1:
		return c(a.Remark < b.Remark)
	case 2:
		return c(a.Whoami < b.Whoami)
	case 3:
		return c(a.Name < b.Name)
	case 4:
		return c(a.Time < b.Time)
	}

	panic("unreachable")
}

func (m *CondomModel) Swap(i, j int) {
	m.items[i], m.items[j] = m.items[j], m.items[i]
}

func NewCondomModel() *CondomModel {
	m := new(CondomModel)
	return m
}
