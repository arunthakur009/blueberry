// Package solver resolves dependency graphs for install/remove operations.
// Uses a simple BFS approach suitable for a curated binary repository.
package solver

import (
	"blueberry.linux/bpm/internal/db"
	"blueberry.linux/bpm/internal/manifest"
	"blueberry.linux/bpm/internal/repo"
	"fmt"
)

// Plan is the result of dependency resolution.
type Plan struct {
	Install []*manifest.Package // ordered: dependencies first
	Remove  []*manifest.Package
	Upgrade []*manifest.Package // already installed but newer version available
}

// Resolver resolves packages against the repo and installed DB.
type Resolver struct {
	db  *db.DB
	mgr *repo.Manager
}

// New creates a Resolver.
func New(database *db.DB, manager *repo.Manager) *Resolver {
	return &Resolver{db: database, mgr: manager}
}

// Resolve returns a plan for installing the named packages.
// Already-installed packages at the same version are skipped.
func (r *Resolver) Resolve(names []string) (*Plan, error) {
	plan := &Plan{}
	visited := make(map[string]bool)
	queue := append([]string(nil), names...)

	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]

		if visited[name] {
			continue
		}
		visited[name] = true

		// Check if already installed
		if r.db.IsInstalled(name) {
			installed, err := r.db.Get(name)
			if err != nil {
				return nil, err
			}
			// Find repo version
			repoPkg, _, err := r.mgr.Find(name)
			if err == nil && newerVersion(repoPkg, installed) {
				plan.Upgrade = append(plan.Upgrade, repoPkg)
				// Still need to resolve deps of new version
				for _, dep := range repoPkg.Depends {
					depName := manifest.DepName(dep)
					if !visited[depName] {
						queue = append(queue, depName)
					}
				}
			}
			// Already installed deps are still visited but not re-added
			for _, dep := range installed.Depends {
				depName := manifest.DepName(dep)
				if !visited[depName] {
					queue = append(queue, depName)
				}
			}
			continue
		}

		// Find in repos
		pkg, _, err := r.mgr.Find(name)
		if err != nil {
			return nil, fmt.Errorf("unresolved dependency: %w", err)
		}

		plan.Install = append(plan.Install, pkg)

		// Queue dependencies
		for _, dep := range pkg.Depends {
			depName := manifest.DepName(dep)
			if !visited[depName] {
				queue = append(queue, depName)
			}
		}
	}

	// Topological sort: dependencies before dependents
	plan.Install = topoSort(plan.Install)
	return plan, nil
}

// ResolveUpgrade returns a plan for upgrading all installed packages.
func (r *Resolver) ResolveUpgrade(names []string) (*Plan, error) {
	if len(names) == 0 {
		var err error
		names, err = r.db.List()
		if err != nil {
			return nil, err
		}
	}

	plan := &Plan{}
	for _, name := range names {
		if !r.db.IsInstalled(name) {
			continue
		}
		installed, err := r.db.Get(name)
		if err != nil {
			continue
		}
		repoPkg, _, err := r.mgr.Find(name)
		if err != nil {
			continue
		}
		if newerVersion(repoPkg, installed) {
			plan.Upgrade = append(plan.Upgrade, repoPkg)
		}
	}
	return plan, nil
}

// ResolveRemove returns the list of packages to remove.
// Fails if another installed package depends on any of them.
func (r *Resolver) ResolveRemove(names []string) (*Plan, error) {
	plan := &Plan{}
	removing := make(map[string]bool)
	for _, n := range names {
		removing[n] = true
	}

	// Check reverse deps
	all, err := r.db.List()
	if err != nil {
		return nil, err
	}
	for _, installedName := range all {
		if removing[installedName] {
			continue
		}
		pkg, err := r.db.Get(installedName)
		if err != nil {
			continue
		}
		for _, dep := range pkg.Depends {
			if removing[manifest.DepName(dep)] {
				return nil, fmt.Errorf(
					"cannot remove %s: required by %s",
					manifest.DepName(dep), installedName,
				)
			}
		}
	}

	for _, n := range names {
		pkg, err := r.db.Get(n)
		if err != nil {
			return nil, err
		}
		plan.Remove = append(plan.Remove, pkg)
	}
	return plan, nil
}

// newerVersion returns true if candidate is newer than installed.
func newerVersion(candidate, installed *manifest.Package) bool {
	if candidate.Version != installed.Version {
		return compareVersions(candidate.Version, installed.Version) > 0
	}
	return candidate.Release > installed.Release
}

// compareVersions does a simple left-to-right numeric segment comparison.
func compareVersions(a, b string) int {
	aParts := splitVersion(a)
	bParts := splitVersion(b)
	max := len(aParts)
	if len(bParts) > max {
		max = len(bParts)
	}
	for i := 0; i < max; i++ {
		var av, bv int
		if i < len(aParts) {
			av = aParts[i]
		}
		if i < len(bParts) {
			bv = bParts[i]
		}
		if av != bv {
			if av > bv {
				return 1
			}
			return -1
		}
	}
	return 0
}

func splitVersion(v string) []int {
	var parts []int
	cur := 0
	hasCur := false
	for _, c := range v {
		if c >= '0' && c <= '9' {
			cur = cur*10 + int(c-'0')
			hasCur = true
		} else {
			if hasCur {
				parts = append(parts, cur)
				cur = 0
				hasCur = false
			}
		}
	}
	if hasCur {
		parts = append(parts, cur)
	}
	return parts
}

// topoSort orders packages so dependencies come before dependents.
// For the binary package sets we manage this is always a DAG.
func topoSort(pkgs []*manifest.Package) []*manifest.Package {
	index := make(map[string]*manifest.Package)
	for _, p := range pkgs {
		index[p.Name] = p
	}

	visited := make(map[string]bool)
	var sorted []*manifest.Package

	var visit func(p *manifest.Package)
	visit = func(p *manifest.Package) {
		if visited[p.Name] {
			return
		}
		visited[p.Name] = true
		for _, dep := range p.Depends {
			name := manifest.DepName(dep)
			if dep, ok := index[name]; ok {
				visit(dep)
			}
		}
		sorted = append(sorted, p)
	}

	for _, p := range pkgs {
		visit(p)
	}
	return sorted
}
