package service

import (
	"encoding/json"
	"testing"
)

// TestStatsDailyReset 验证今日计数跨天归零、累计计数保留。
func TestStatsDailyReset(t *testing.T) {
	s := NewStats()
	s.RecordRequest()
	s.RecordSuccess()

	snap := s.Snapshot()
	if snap.TotalRequests != 1 || snap.TodayRequests != 1 {
		t.Errorf("requests: total=%d today=%d, want 1/1", snap.TotalRequests, snap.TodayRequests)
	}
	if snap.TotalSuccess != 1 || snap.TodaySuccess != 1 {
		t.Errorf("success: total=%d today=%d, want 1/1", snap.TotalSuccess, snap.TodaySuccess)
	}

	// 模拟跨天：把 dayKey 拨回过去，下一次记录应重置当日计数
	s.dayMu.Lock()
	s.dayKey = "2000-01-01"
	s.dayMu.Unlock()

	s.RecordFail()
	snap = s.Snapshot()

	if snap.TodayFail != 1 {
		t.Errorf("today fail = %d, want 1 (fresh day)", snap.TodayFail)
	}
	if snap.TodayRequests != 0 || snap.TodaySuccess != 0 {
		t.Errorf("today counters not reset: requests=%d success=%d", snap.TodayRequests, snap.TodaySuccess)
	}
	if snap.TotalRequests != 1 || snap.TotalFail != 1 {
		t.Errorf("total counters lost after day roll: requests=%d fail=%d", snap.TotalRequests, snap.TotalFail)
	}
}

// TestStatsCircuitTrip 验证熔断计数同时计入累计与今日。
func TestStatsCircuitTrip(t *testing.T) {
	s := NewStats()
	s.RecordCircuitTrip()
	snap := s.Snapshot()
	if snap.CircuitRejected != 1 || snap.TodayTrips != 1 {
		t.Errorf("rejected: total=%d today=%d, want 1/1", snap.CircuitRejected, snap.TodayTrips)
	}
}

// TestStatsSnapshotCircuitRejectedJSON 回归 C-02：快照 JSON 字段为 circuit_rejected，
// 不再与 checks.circuit_trips（熔断器进入 OPEN 的次数）同名。
func TestStatsSnapshotCircuitRejectedJSON(t *testing.T) {
	s := NewStats()
	s.RecordCircuitTrip()

	b, err := json.Marshal(s.Snapshot())
	if err != nil {
		t.Fatalf("快照无法序列化: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("快照不是合法 JSON: %v", err)
	}
	if v, ok := got["circuit_rejected"]; !ok || v != float64(1) {
		t.Errorf("circuit_rejected = %v (存在=%v), want 1", v, ok)
	}
	if _, ok := got["circuit_trips"]; ok {
		t.Error("circuit_trips 不该再出现在 stats 快照里")
	}
}
