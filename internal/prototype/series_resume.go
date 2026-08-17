package prototype

import (
	"strings"
	"time"
)

type seriesResumeRank struct {
	meaningful bool
	timestamp  time.Time
	rawTime    string
}

func seriesResumeMeaningful(completed any, index any, splitPanel any, scrollTop any, scrollLeft any) bool {
	return boolValue(completed) ||
		intValue(index) > 0 ||
		intValue(splitPanel) > 0 ||
		intValue(scrollTop) > 0 ||
		intValue(scrollLeft) > 0
}

func newSeriesResumeRank(
	completed any,
	index any,
	splitPanel any,
	scrollTop any,
	scrollLeft any,
	lastReadAt any,
	updatedAt any,
) seriesResumeRank {
	rawTime := strings.TrimSpace(coalesceString(lastReadAt, updatedAt))
	return seriesResumeRank{
		meaningful: seriesResumeMeaningful(completed, index, splitPanel, scrollTop, scrollLeft),
		timestamp:  browseStateTime(rawTime),
		rawTime:    rawTime,
	}
}

func seriesResumeRankFromProgress(progress map[string]any) seriesResumeRank {
	if progress == nil {
		return seriesResumeRank{}
	}
	return newSeriesResumeRank(
		progress["completed"],
		progress["index"],
		progress["reader_split_panel"],
		progress["stage_scroll_top"],
		progress["stage_scroll_left"],
		progress["last_read_at"],
		progress["updated_at"],
	)
}

func seriesResumeRankFromRow(row map[string]any) seriesResumeRank {
	if row == nil {
		return seriesResumeRank{}
	}
	return newSeriesResumeRank(
		row["completed"],
		row["last_page_index"],
		row["reader_split_panel"],
		row["stage_scroll_top"],
		row["stage_scroll_left"],
		row["last_read_at"],
		row["updated_at"],
	)
}

func compareSeriesResumeRanks(left, right seriesResumeRank) int {
	if left.meaningful != right.meaningful {
		if left.meaningful {
			return 1
		}
		return -1
	}
	leftValid := !left.timestamp.IsZero()
	rightValid := !right.timestamp.IsZero()
	if leftValid != rightValid {
		if leftValid {
			return 1
		}
		return -1
	}
	if leftValid && !left.timestamp.Equal(right.timestamp) {
		if left.timestamp.After(right.timestamp) {
			return 1
		}
		return -1
	}
	if left.rawTime > right.rawTime {
		return 1
	}
	if left.rawTime < right.rawTime {
		return -1
	}
	return 0
}

func compareSeriesResumeProgress(left, right map[string]any) int {
	return compareSeriesResumeRanks(
		seriesResumeRankFromProgress(left),
		seriesResumeRankFromProgress(right),
	)
}

func seriesResumeProgressRowBetter(candidate, current map[string]any) bool {
	if current == nil {
		return candidate != nil
	}
	if comparison := compareSeriesResumeRanks(
		seriesResumeRankFromRow(candidate),
		seriesResumeRankFromRow(current),
	); comparison != 0 {
		return comparison > 0
	}
	candidateSequence := numericFloat(candidate["series_progress_sequence"], 0)
	currentSequence := numericFloat(current["series_progress_sequence"], 0)
	if candidateSequence != currentSequence {
		return candidateSequence > currentSequence
	}
	candidateSort := stringValue(candidate["series_progress_sort_key"])
	currentSort := stringValue(current["series_progress_sort_key"])
	if candidateSort != currentSort {
		return candidateSort > currentSort
	}
	return stringValue(candidate["candidate_id"]) > stringValue(current["candidate_id"])
}
