// Package planning provides network analysis and planning services.
package planning

import (
	"net/netip"
	"time"
)

// AnalysisRequest specifies which pools to analyze.
type AnalysisRequest struct {
	PoolIDs         []int64 `json:"pool_ids,omitempty"`
	IncludeChildren bool    `json:"include_children"`
}

// GapAnalysisRequest specifies a single pool for gap analysis.
type GapAnalysisRequest struct {
	PoolID int64 `json:"pool_id"`
}

// GapAnalysis describes the used and free address space within a parent pool.
type GapAnalysis struct {
	PoolID          int64            `json:"pool_id"`
	PoolName        string           `json:"pool_name"`
	ParentCIDR      string           `json:"parent_cidr"`
	AllocatedBlocks []AllocatedBlock `json:"allocated_blocks"`
	AvailableBlocks []AvailableBlock `json:"available_blocks"`
	TotalAddresses  uint64           `json:"total_addresses"`
	UsedAddresses   uint64           `json:"used_addresses"`
	FreeAddresses   uint64           `json:"free_addresses"`
	Utilization     float64          `json:"utilization_percent"`
}

// AllocatedBlock represents a child pool's allocation within a parent.
type AllocatedBlock struct {
	PoolID      int64   `json:"pool_id"`
	Name        string  `json:"name"`
	CIDR        string  `json:"cidr"`
	Utilization float64 `json:"utilization_percent"`
}

// AvailableBlock represents an unallocated CIDR range.
type AvailableBlock struct {
	CIDR         string `json:"cidr"`
	AddressCount uint64 `json:"address_count"`
}

// FragmentationAnalysis scores how fragmented a pool's address space is.
type FragmentationAnalysis struct {
	PoolID          int64                `json:"pool_id"`
	PoolName        string               `json:"pool_name"`
	Score           int                  `json:"score"` // 0 (no fragmentation) to 100 (severe)
	Issues          []FragmentationIssue `json:"issues"`
	Recommendations []string             `json:"recommendations"`
}

// FragmentationType categorizes fragmentation issues.
type FragmentationType string

const (
	FragmentScattered  FragmentationType = "scattered"
	FragmentOversized  FragmentationType = "oversized"
	FragmentUndersized FragmentationType = "undersized"
	FragmentMisaligned FragmentationType = "misaligned"
)

// FragmentationIssue describes a specific fragmentation problem.
type FragmentationIssue struct {
	Type        FragmentationType `json:"type"`
	Severity    string            `json:"severity"` // error, warning, info
	CIDR        string            `json:"cidr"`
	PoolID      int64             `json:"pool_id"`
	Description string            `json:"description"`
}

// ComplianceReport summarizes compliance check results for a set of pools.
type ComplianceReport struct {
	TotalChecks int                   `json:"total_checks"`
	Passed      int                   `json:"passed"`
	Failed      int                   `json:"failed"`
	Warnings    int                   `json:"warnings"`
	Violations  []ComplianceViolation `json:"violations"`
}

// ComplianceViolation describes a specific compliance failure.
type ComplianceViolation struct {
	RuleID      string `json:"rule_id"`
	Severity    string `json:"severity"` // error, warning, info
	PoolID      int64  `json:"pool_id"`
	PoolName    string `json:"pool_name"`
	CIDR        string `json:"cidr,omitempty"`
	Message     string `json:"message"`
	Remediation string `json:"remediation,omitempty"`
}

// NetworkAnalysisReport is the combined output of all analysis passes.
type NetworkAnalysisReport struct {
	GeneratedAt   time.Time              `json:"generated_at"`
	Summary       AnalysisSummary        `json:"summary"`
	GapAnalyses   []GapAnalysis          `json:"gap_analyses"`
	Fragmentation *FragmentationAnalysis `json:"fragmentation,omitempty"`
	Compliance    *ComplianceReport      `json:"compliance,omitempty"`
}

// AnalysisSummary provides high-level metrics across all analyzed pools.
type AnalysisSummary struct {
	TotalPools         int     `json:"total_pools"`
	TotalAddresses     uint64  `json:"total_addresses"`
	UsedAddresses      uint64  `json:"used_addresses"`
	AvailableAddresses uint64  `json:"available_addresses"`
	Utilization        float64 `json:"utilization_percent"`
	HealthScore        int     `json:"health_score"` // 0-100
	ErrorCount         int     `json:"error_count"`
	WarningCount       int     `json:"warning_count"`
	InfoCount          int     `json:"info_count"`
}

// interval is an internal type representing a uint32 address range [start, end] inclusive.
type interval struct {
	start uint32
	end   uint32
}

// prefixToInterval converts a netip.Prefix to a uint32 interval.
func prefixToInterval(p netip.Prefix) interval {
	start := ipv4ToUint32(p.Masked().Addr())
	size := uint32(1) << (32 - p.Bits())
	return interval{start: start, end: start + size - 1}
}
