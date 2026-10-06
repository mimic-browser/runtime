package optimize

// An uncovered trial is never promoted. A different supplied reference plan may
// avoid that branch and earn its own covered PASS. Covered assertion failures,
// corrupt evidence, unsupported protocols and clients on another endpoint are
// not treated as opportunities to hide instability.
func canProbeReferenceReplay(row Run, supplied bool) bool {
	return supplied && row.Status == "UNSUPPORTED" && row.Metrics.CDPConnections > 0 &&
		len(row.Metrics.CaptureMisses) > 0 && len(row.Metrics.Violations) == 0 &&
		len(row.Metrics.Unsupported) == 0 && row.WorkloadStatus == "FAIL"
}
