package snapshot

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// localIndexes holds case-insensitive lookups over the current (local) core state.
type localIndexes struct {
	buckets map[string]Bucket   // lower(trimmed name) -> locally recorded bucket
	byKey   map[string]Package  // name@version -> package
	names   map[string]struct{} // lower(trimmed name) -> present at any version
	current map[string]string   // lower(trimmed name) -> active version
}

// indexLocalState builds the lookup tables shared by the plan helpers.
func indexLocalState(current CoreState) localIndexes {
	idx := localIndexes{
		buckets: make(map[string]Bucket),
		byKey:   make(map[string]Package),
		names:   make(map[string]struct{}),
		current: make(map[string]string),
	}
	for _, b := range current.Buckets {
		name := strings.TrimSpace(b.Name)
		if name == "" {
			continue
		}
		idx.buckets[strings.ToLower(name)] = b
	}
	for _, p := range current.Packages {
		name := strings.TrimSpace(p.Name)
		if name == "" {
			continue
		}
		nameKey := strings.ToLower(name)
		idx.names[nameKey] = struct{}{}
		idx.byKey[packageKey(name, p.Version)] = p
		if p.Current {
			idx.current[nameKey] = strings.TrimSpace(p.Version)
		}
	}
	return idx
}

// planBucketAdds records target buckets the local state does not have yet.
func planBucketAdds(plan *Plan, local localIndexes, target CoreState) {
	for _, b := range target.Buckets {
		name := strings.TrimSpace(b.Name)
		if name == "" {
			continue
		}
		if _, ok := local.buckets[strings.ToLower(name)]; ok {
			continue
		}
		plan.BucketsToAdd = append(plan.BucketsToAdd, Bucket{Name: name, URL: strings.TrimSpace(b.URL)})
	}
}

// planPackageInstalls records target package versions missing locally.
func planPackageInstalls(plan *Plan, local localIndexes, target CoreState) {
	for _, p := range target.Packages {
		name := strings.TrimSpace(p.Name)
		if name == "" {
			continue
		}
		version := strings.TrimSpace(p.Version)
		if locallySatisfied(local, name, version) {
			continue
		}
		plan.PackagesToInstall = append(plan.PackagesToInstall, Package{
			Name:          name,
			Bucket:        strings.TrimSpace(p.Bucket),
			Version:       version,
			Current:       p.Current,
			VersionLocked: p.VersionLocked,
		})
	}
}

// locallySatisfied reports whether the local state already covers a target
// intent: a versionless target is covered by any local version, a versioned one
// only by that exact version.
func locallySatisfied(local localIndexes, name, version string) bool {
	if version == "" {
		_, ok := local.names[strings.ToLower(name)]
		return ok
	}
	_, ok := local.byKey[packageKey(name, version)]
	return ok
}

// planPackageActivations records target versions that must become the active one.
func planPackageActivations(plan *Plan, local localIndexes, target CoreState) {
	for _, p := range target.Packages {
		if !p.Current {
			continue
		}
		name := strings.TrimSpace(p.Name)
		version := strings.TrimSpace(p.Version)
		if name == "" || version == "" {
			continue
		}
		if local.current[strings.ToLower(name)] == version {
			continue
		}
		plan.PackagesToActivate = append(plan.PackagesToActivate, Package{
			Name:          name,
			Bucket:        strings.TrimSpace(p.Bucket),
			Version:       version,
			Current:       true,
			VersionLocked: p.VersionLocked,
		})
	}
}

// planRemovals records local buckets and packages absent from target. Removal is
// destructive, so the caller must confirm before applying.
func planRemovals(plan *Plan, current CoreState, local localIndexes, target CoreState) {
	planBucketRemovals(plan, local, target)
	planPackageRemovals(plan, current, local, target)
	sort.Strings(plan.BucketsToRemove)
	sort.Strings(plan.PackagesToRemove)
}

// planBucketRemovals records locally known buckets missing from target.
func planBucketRemovals(plan *Plan, local localIndexes, target CoreState) {
	wanted := map[string]struct{}{}
	for _, b := range target.Buckets {
		wanted[strings.ToLower(strings.TrimSpace(b.Name))] = struct{}{}
	}
	for key, b := range local.buckets {
		if _, ok := wanted[key]; !ok {
			plan.BucketsToRemove = append(plan.BucketsToRemove, b.Name)
		}
	}
}

// planPackageRemovals records locally installed package names missing from target.
func planPackageRemovals(plan *Plan, current CoreState, local localIndexes, target CoreState) {
	wanted := map[string]struct{}{}
	for _, p := range target.Packages {
		wanted[strings.ToLower(strings.TrimSpace(p.Name))] = struct{}{}
	}
	seen := map[string]struct{}{}
	for nameKey := range local.names {
		if _, ok := wanted[nameKey]; ok {
			continue
		}
		display := localDisplayName(current.Packages, nameKey)
		if _, ok := seen[strings.ToLower(display)]; ok {
			continue
		}
		seen[strings.ToLower(display)] = struct{}{}
		plan.PackagesToRemove = append(plan.PackagesToRemove, display)
	}
}

// localDisplayName returns the spelling of the first local entry matching
// nameKey, falling back to the lowercase key when no entry matches.
func localDisplayName(packages []Package, nameKey string) string {
	for _, p := range packages {
		if strings.EqualFold(strings.TrimSpace(p.Name), nameKey) {
			return strings.TrimSpace(p.Name)
		}
	}
	return nameKey
}

// sortPlan orders every action list so identical inputs produce identical plans.
func sortPlan(plan *Plan) {
	sort.Slice(plan.BucketsToAdd, func(i, j int) bool {
		return strings.ToLower(plan.BucketsToAdd[i].Name) < strings.ToLower(plan.BucketsToAdd[j].Name)
	})
	sort.Slice(plan.PackagesToInstall, func(i, j int) bool {
		a, b := plan.PackagesToInstall[i], plan.PackagesToInstall[j]
		an, bn := strings.ToLower(a.Name), strings.ToLower(b.Name)
		if an != bn {
			return an < bn
		}
		return a.Version < b.Version
	})
	sort.Slice(plan.PackagesToActivate, func(i, j int) bool {
		return strings.ToLower(plan.PackagesToActivate[i].Name) < strings.ToLower(plan.PackagesToActivate[j].Name)
	})
}

// Diff compares current local core state against a target snapshot core state.
//
// install-missing (default):
//   - add missing buckets
//   - install package versions not present locally (same name, different version = missing version)
//   - switch active (current) version when the snapshot marks a different one
//   - apply config differences
//
// reconcile:
//   - also remove local packages/buckets not in target (caller must confirm)
func Diff(current CoreState, target CoreState, opts ApplyOptions) (*Plan, error) {
	plan := &Plan{}
	local := indexLocalState(current)

	planBucketAdds(plan, local, target)
	planPackageInstalls(plan, local, target)
	planPackageActivations(plan, local, target)

	plan.ConfigChanges = diffConfig(current.Config, target.Config)

	if NormalizeMode(opts.Mode) == ApplyModeReconcile {
		planRemovals(plan, current, local, target)
	}

	sortPlan(plan)
	return plan, nil
}

func packageKey(name, version string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	version = strings.TrimSpace(version)
	if version == "" {
		return name
	}
	return name + "@" + version
}

func diffConfig(current, target Config) []ConfigChange {
	var out []ConfigChange
	if cur, want := strings.TrimSpace(current.GitHubProxy), strings.TrimSpace(target.GitHubProxy); cur != want {
		// Only propose a change when target explicitly sets a value, or current has one to clear.
		if want != "" || cur != "" {
			out = append(out, ConfigChange{Key: "githubProxy", From: cur, To: want})
		}
	}
	curWorkers, curOK := workersString(current.DownloadWorkers)
	wantWorkers, wantOK := workersString(target.DownloadWorkers)
	if wantOK && curWorkers != wantWorkers {
		from := ""
		if curOK {
			from = curWorkers
		}
		out = append(out, ConfigChange{Key: "downloadWorkers", From: from, To: wantWorkers})
	}
	if cur, want := strings.TrimSpace(current.BucketSyncMode), strings.TrimSpace(target.BucketSyncMode); want != "" && cur != want {
		out = append(out, ConfigChange{Key: "bucketSyncMode", From: cur, To: want})
	}
	return out
}

func workersString(v *int) (string, bool) {
	if v == nil {
		return "", false
	}
	return strconv.Itoa(*v), true
}

// InstallRef builds an engine InstallRequest name: [bucket/]name[@version].
func InstallRef(pkg Package) string {
	name := strings.TrimSpace(pkg.Name)
	bucket := strings.TrimSpace(pkg.Bucket)
	version := strings.TrimSpace(pkg.Version)
	ref := name
	if bucket != "" && !strings.EqualFold(bucket, "main") {
		ref = bucket + "/" + name
	}
	if version != "" {
		ref = ref + "@" + version
	}
	return ref
}

// ResolveBucketURL returns URL from snapshot entry or known buckets.
func ResolveBucketURL(name, url string, known func(string) (string, bool)) (string, error) {
	name = strings.TrimSpace(name)
	url = strings.TrimSpace(url)
	if url != "" {
		return url, nil
	}
	if known != nil {
		if u, ok := known(name); ok && strings.TrimSpace(u) != "" {
			return strings.TrimSpace(u), nil
		}
	}
	return "", fmt.Errorf("bucket %q has no URL and is not a known bucket", name)
}
