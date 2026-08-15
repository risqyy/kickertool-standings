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
	ID                 uint
	RequestedPlayerID  uint
	CanonicalNameKey   string
	DisplayName        string
	Aliases            []PlayerAlias
	MatchedAlias       string
	Aggregate          PlayerAggregate
	Active             bool
	MergedIntoPlayerID *uint
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
