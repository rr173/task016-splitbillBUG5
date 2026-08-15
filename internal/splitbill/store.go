// 属于 splitbill 包：定义账单、组与内存存储。
package splitbill

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
)

// 存储相关错误。
var (
	ErrGroupNotFound      = errors.New("splitbill: 组不存在")
	ErrDuplicateMember    = errors.New("splitbill: 组内成员不能重复")
	ErrPayerNotMember     = errors.New("splitbill: 付款人必须是组成员")
	ErrParticipantNotMem  = errors.New("splitbill: 参与者必须是组成员")
	ErrDuplicateParticipant = errors.New("splitbill: 同一账单内参与者不能重复")
	ErrEmptyMembers       = errors.New("splitbill: 组成员不能为空")
)

// Bill 已记录的一笔账单。
type Bill struct {
	ID          string
	Payer       string
	AmountCents int64
	Mode        Mode
	Shares      []Share
}

// Group 一个分摊组。
type Group struct {
	ID      string
	Members []string
	Bills   []Bill
}

// Store 是并发安全的内存存储，按组标识存放组与账单。
type Store struct {
	mu     sync.RWMutex
	groups map[string]*Group
	next   atomic.Uint64
}

// NewStore 创建空存储。
func NewStore() *Store {
	return &Store{groups: make(map[string]*Group)}
}

// CreateGroup 创建一个包含给定成员的组，返回组标识。
// 成员列表不能为空且不能有重复。
func (s *Store) CreateGroup(members []string) (string, error) {
	if len(members) == 0 {
		return "", ErrEmptyMembers
	}
	seen := make(map[string]struct{}, len(members))
	for _, m := range members {
		if _, ok := seen[m]; ok {
			return "", ErrDuplicateMember
		}
		seen[m] = struct{}{}
	}
	id := fmt.Sprintf("%d", s.next.Add(1))
	g := &Group{ID: id, Members: members}
	s.mu.Lock()
	s.groups[id] = g
	s.mu.Unlock()
	return id, nil
}

// AddBill 校验并记录一笔账单到指定组，返回计算出的份额与账单记录。
func (s *Store) AddBill(groupID string, payer string, amount int64, mode Mode, participants []ParticipantInput) (Bill, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.groups[groupID]
	if !ok {
		return Bill{}, ErrGroupNotFound
	}
	memberSet := make(map[string]struct{}, len(g.Members))
	for _, m := range g.Members {
		memberSet[m] = struct{}{}
	}
	if _, ok := memberSet[payer]; !ok {
		return Bill{}, ErrPayerNotMember
	}
	if len(participants) == 0 {
		return Bill{}, ErrEmptyParticipants
	}
	for _, p := range participants {
		if _, ok := memberSet[p.Name]; !ok {
			return Bill{}, ErrParticipantNotMem
		}
	}

	shares, err := ComputeShares(amount, mode, participants)
	if err != nil {
		return Bill{}, err
	}
	// 防御性校验：份额不变量必须成立。
	if err := ValidateShares(amount, shares); err != nil {
		return Bill{}, err
	}

	bill := Bill{
		ID:          fmt.Sprintf("%s-%d", groupID, len(g.Bills)+1),
		Payer:       payer,
		AmountCents: amount,
		Mode:        mode,
		Shares:      shares,
	}
	g.Bills = append(g.Bills, bill)
	return bill, nil
}

// Balance 返回组内每个成员的累计净额。
func (s *Store) Balance(groupID string) (map[string]int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	g, ok := s.groups[groupID]
	if !ok {
		return nil, ErrGroupNotFound
	}
	return NetBalance(g.Members, g.Bills), nil
}

// Settlement 返回使组内净额清零的转账方案。
func (s *Store) Settlement(groupID string) ([]Transfer, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	g, ok := s.groups[groupID]
	if !ok {
		return nil, ErrGroupNotFound
	}
	bal := NetBalance(g.Members, g.Bills)
	return Settle(bal), nil
}
