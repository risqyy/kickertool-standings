package domain

import "time"

type PlayerMergeOptions struct {
	DryRun bool
	Actor  string
	Reason string
}

type PlayerAlias struct {
	NameKey     string
	DisplayName string
}

type PlayerProfile struct {
	ID                       uint
	RequestedPlayerID        uint
	CanonicalNameKey         string
	DisplayName              string
	Aliases                  []PlayerAlias
	MatchedAlias             string
	Aggregate                PlayerAggregate
	Active                   bool
	MergedIntoPlayerID       *uint
	RankingCorrectionVersion int64
}

type MergeResult struct {
	SourcePlayerID              uint
	TargetPlayerID              uint
	SourceDisplayName           string
	TargetDisplayName           string
	AlreadyMerged               bool
	DryRun                      bool
	TransferredAliases          int
	TransferredSourceIdentities int
	TransferredAllocations      int
	DeduplicatedAllocations     int
	RecalculatedAggregates      int
	MergedAt                    time.Time
	SourceBefore                *PlayerAggregate
	TargetBefore                *PlayerAggregate
	TargetAfter                 *PlayerAggregate
}

type PlayerMergeAudit struct {
	ID                          uint
	SourcePlayerID              uint
	TargetPlayerID              uint
	SourceDisplayName           string
	TargetDisplayName           string
	MergedAt                    time.Time
	TransferredAliases          int
	TransferredSourceIdentities int
	TransferredAllocations      int
	DeduplicatedAllocations     int
	Actor                       string
	Reason                      string
	UndoAvailable               bool
	UndoUnavailableReason       string
	UndoneAt                    *time.Time
	UndoneBy                    string
	UndoReason                  string
}

type PlayerMergeUndoPreview struct {
	Merge            PlayerMergeAudit
	SourceBefore     PlayerAggregate
	TargetBefore     PlayerAggregate
	StateFingerprint string
}

type PlayerMergeUndoOptions struct {
	Actor               string
	Reason              string
	ExpectedFingerprint string
}

type PlayerMergeUndoResult struct {
	Merge       PlayerMergeAudit
	SourceAfter PlayerAggregate
	TargetAfter PlayerAggregate
	UndoneAt    time.Time
}
