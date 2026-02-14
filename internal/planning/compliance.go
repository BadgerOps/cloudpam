package planning

import (
	"context"
	"fmt"
	"net/netip"

	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

// CheckCompliance validates a set of pools against compliance rules.
func (s *AnalysisService) CheckCompliance(ctx context.Context, poolIDs []int64, includeChildren bool) (*ComplianceReport, error) {
	pools, err := s.resolvePools(ctx, poolIDs, includeChildren)
	if err != nil {
		return nil, err
	}

	report := &ComplianceReport{}

	for _, pool := range pools {
		s.checkOverlap(ctx, pool, pools, report)
		s.checkRFC1918(pool, report)
		s.checkEmpty(ctx, pool, report)
		s.checkNaming(pool, report)
	}

	report.Passed = report.TotalChecks - report.Failed - report.Warnings
	return report, nil
}

// checkOverlap detects overlapping sibling CIDRs (OVERLAP-001).
func (s *AnalysisService) checkOverlap(ctx context.Context, pool domain.Pool, allPools []domain.Pool, report *ComplianceReport) {
	report.TotalChecks++

	if pool.ParentID == nil {
		return
	}

	// Find siblings (same parent, different ID).
	for _, sibling := range allPools {
		if sibling.ID == pool.ID || sibling.ParentID == nil || *sibling.ParentID != *pool.ParentID {
			continue
		}
		pp, err1 := netip.ParsePrefix(pool.CIDR)
		sp, err2 := netip.ParsePrefix(sibling.CIDR)
		if err1 != nil || err2 != nil {
			continue
		}
		if prefixesOverlap(pp.Masked(), sp.Masked()) {
			report.Failed++
			report.Violations = append(report.Violations, ComplianceViolation{
				RuleID:      "OVERLAP-001",
				Severity:    "error",
				PoolID:      pool.ID,
				PoolName:    pool.Name,
				CIDR:        pool.CIDR,
				Message:     fmt.Sprintf("overlaps with sibling %q (%s)", sibling.Name, sibling.CIDR),
				Remediation: "Remove or resize one of the overlapping pools",
			})
			return // report once per pool
		}
	}
}

// checkRFC1918 flags non-RFC1918 space in pools (RFC1918-001).
func (s *AnalysisService) checkRFC1918(pool domain.Pool, report *ComplianceReport) {
	report.TotalChecks++

	p, err := netip.ParsePrefix(pool.CIDR)
	if err != nil {
		return
	}
	if !isRFC1918(p.Masked()) {
		report.Warnings++
		report.Violations = append(report.Violations, ComplianceViolation{
			RuleID:      "RFC1918-001",
			Severity:    "warning",
			PoolID:      pool.ID,
			PoolName:    pool.Name,
			CIDR:        pool.CIDR,
			Message:     "pool uses non-RFC1918 address space",
			Remediation: "Verify this is intentional; private infrastructure typically uses 10/8, 172.16/12, or 192.168/16",
		})
	}
}

// checkEmpty flags parent pools with no children (EMPTY-001).
func (s *AnalysisService) checkEmpty(ctx context.Context, pool domain.Pool, report *ComplianceReport) {
	report.TotalChecks++

	children, err := s.store.GetPoolChildren(ctx, pool.ID)
	if err != nil {
		return
	}
	// Only flag pools that look like parents (supernet/region/environment/vpc types).
	isParentType := pool.Type == domain.PoolTypeSupernet ||
		pool.Type == domain.PoolTypeRegion ||
		pool.Type == domain.PoolTypeEnvironment ||
		pool.Type == domain.PoolTypeVPC

	if isParentType && len(children) == 0 {
		report.Warnings++
		report.Violations = append(report.Violations, ComplianceViolation{
			RuleID:      "EMPTY-001",
			Severity:    "warning",
			PoolID:      pool.ID,
			PoolName:    pool.Name,
			CIDR:        pool.CIDR,
			Message:     fmt.Sprintf("pool of type %q has no child allocations", pool.Type),
			Remediation: "Add child pools or change the pool type to 'subnet'",
		})
	}
}

// checkNaming flags missing pool names and descriptions (NAME-001, NAME-002).
func (s *AnalysisService) checkNaming(pool domain.Pool, report *ComplianceReport) {
	report.TotalChecks++
	if pool.Name == "" {
		report.Warnings++
		report.Violations = append(report.Violations, ComplianceViolation{
			RuleID:   "NAME-001",
			Severity: "info",
			PoolID:   pool.ID,
			PoolName: pool.Name,
			CIDR:     pool.CIDR,
			Message:  "pool has no name",
		})
	}

	report.TotalChecks++
	if pool.Description == "" {
		report.Violations = append(report.Violations, ComplianceViolation{
			RuleID:   "NAME-002",
			Severity: "info",
			PoolID:   pool.ID,
			PoolName: pool.Name,
			CIDR:     pool.CIDR,
			Message:  "pool has no description",
		})
	}
}

// resolvePools fetches the requested pools and optionally their children.
func (s *AnalysisService) resolvePools(ctx context.Context, poolIDs []int64, includeChildren bool) ([]domain.Pool, error) {
	var pools []domain.Pool

	if len(poolIDs) == 0 {
		// All pools.
		all, err := s.store.ListPools(ctx)
		if err != nil {
			return nil, fmt.Errorf("list pools: %w", err)
		}
		pools = all
	} else {
		for _, id := range poolIDs {
			p, found, err := s.store.GetPool(ctx, id)
			if err != nil {
				return nil, fmt.Errorf("get pool %d: %w", id, err)
			}
			if !found {
				return nil, fmt.Errorf("pool %d: %w", id, storage.ErrNotFound)
			}
			pools = append(pools, p)
		}
	}

	if includeChildren {
		seen := map[int64]bool{}
		for _, p := range pools {
			seen[p.ID] = true
		}
		var extra []domain.Pool
		for _, p := range pools {
			children, err := s.store.GetPoolChildren(ctx, p.ID)
			if err != nil {
				continue
			}
			for _, c := range children {
				if !seen[c.ID] {
					seen[c.ID] = true
					extra = append(extra, c)
				}
			}
		}
		pools = append(pools, extra...)
	}

	return pools, nil
}
