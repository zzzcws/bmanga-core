package prototype

import "testing"

func TestSeriesResumeRankPrefersMeaningfulProgressOverNewerPageOneOpen(t *testing.T) {
	meaningful := newSeriesResumeRank(
		0, 3, 0, 0, 0,
		"2026-07-20T04:00:00Z", "",
	)
	pageOne := newSeriesResumeRank(
		0, 0, 0, 0, 0,
		"2026-07-20T12:30:00+08:00", "",
	)
	if comparison := compareSeriesResumeRanks(meaningful, pageOne); comparison <= 0 {
		t.Fatalf("meaningful progress comparison = %d, want > 0", comparison)
	}
}

func TestSeriesResumeRankComparesAbsoluteTimeAfterMeaningfulClass(t *testing.T) {
	earlier := newSeriesResumeRank(
		0, 2, 0, 0, 0,
		"2026-07-20T12:30:00+08:00", "",
	)
	later := newSeriesResumeRank(
		0, 4, 0, 0, 0,
		"2026-07-20T05:00:00Z", "",
	)
	if comparison := compareSeriesResumeRanks(later, earlier); comparison <= 0 {
		t.Fatalf("later absolute time comparison = %d, want > 0", comparison)
	}
}

func TestSeriesResumeMeaningfulIncludesSplitPanelAndStageScroll(t *testing.T) {
	if !seriesResumeMeaningful(0, 0, 1, 0, 0) {
		t.Fatal("split panel progress should be meaningful")
	}
	if !seriesResumeMeaningful(0, 0, 0, 320, 0) {
		t.Fatal("stage scroll progress should be meaningful")
	}
	if seriesResumeMeaningful(0, 0, 0, 0, 0) {
		t.Fatal("plain page-one open should not be meaningful")
	}
}
