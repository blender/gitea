// Copyright 2025 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

// BLENDER: spam reporting

package user

import (
	"context"
	"fmt"

	"code.gitea.io/gitea/models/db"
	"code.gitea.io/gitea/modules/timeutil"
)

// SpamReportStatusType is used to support a spam report lifecycle:
//
// pending -> locked
// locked -> processed | dismissed
//
// "locked" status works as a lock for a record that is being processed.
type SpamReportStatusType int

const (
	SpamReportStatusTypePending   = iota // 0
	SpamReportStatusTypeLocked           // 1
	SpamReportStatusTypeProcessed        // 2
	SpamReportStatusTypeDismissed        // 3
)

func (t SpamReportStatusType) String() string {
	switch t {
	case SpamReportStatusTypePending:
		return "pending"
	case SpamReportStatusTypeLocked:
		return "locked"
	case SpamReportStatusTypeProcessed:
		return "processed"
	case SpamReportStatusTypeDismissed:
		return "dismissed"
	}
	return "unknown"
}

type SpamReport struct {
	ID          int64                `xorm:"pk autoincr"`
	UserID      int64                `xorm:"UNIQUE"`
	ReporterID  int64                `xorm:"NOT NULL"`
	Status      SpamReportStatusType `xorm:"INDEX NOT NULL DEFAULT 0"`
	CreatedUnix timeutil.TimeStamp   `xorm:"created"`
	UpdatedUnix timeutil.TimeStamp   `xorm:"updated"`
}

func (*SpamReport) TableName() string {
	return "user_spamreport"
}

func init() {
	// This table doesn't exist in the upstream code.
	// We don't introduce migrations for it to avoid migration id clashes.
	// Gitea will create the table in the database during startup,
	// so no manual action is required until we start modifying the table.
	db.RegisterModel(new(SpamReport))
}

type ListSpamReportsOptions struct {
	db.ListOptions
	Status SpamReportStatusType
}

type ListSpamReportsResults struct {
	ID           int64
	CreatedUnix  timeutil.TimeStamp
	UpdatedUnix  timeutil.TimeStamp
	Status       SpamReportStatusType
	UserName     string
	ReporterName string
}

func ListSpamReports(ctx context.Context, opts *ListSpamReportsOptions) ([]*ListSpamReportsResults, int64, error) {
	opts.SetDefaultValues()
	count, err := db.GetEngine(ctx).Count(new(SpamReport))
	if err != nil {
		return nil, 0, fmt.Errorf("Count: %w", err)
	}
	spamReports := make([]*ListSpamReportsResults, 0, opts.PageSize)
	err = db.GetEngine(ctx).Table("user_spamreport").
		Select("user_spamreport.id, user_spamreport.created_unix, user_spamreport.updated_unix, user_spamreport.status, `user`.name as user_name, reporter.name as reporter_name").
		Join("LEFT", "`user`", "`user`.id = user_spamreport.user_id").
		Join("LEFT", "`user` as reporter", "`reporter`.id = user_spamreport.reporter_id").
		Where("status = ?", opts.Status).
		Limit(opts.PageSize, (opts.Page-1)*opts.PageSize).
		Find(&spamReports)

	return spamReports, count, err
}

func GetPendingSpamReportIDs(ctx context.Context) ([]int64, error) {
	var ids []int64
	err := db.GetEngine(ctx).Table("user_spamreport").
		Select("id").Where("status = ?", SpamReportStatusTypePending).Find(&ids)
	return ids, err
}

type SpamReportStatusCounts struct {
	Count  int64
	Status SpamReportStatusType
}

func GetSpamReportStatusCounts(ctx context.Context) ([]*SpamReportStatusCounts, error) {
	statusCounts := make([]*SpamReportStatusCounts, 0, 4) // 4 status types
	err := db.GetEngine(ctx).Table("user_spamreport").
		Select("count(*) as count, status").
		GroupBy("status").
		Find(&statusCounts)

	return statusCounts, err
}

func GetSpamReportForUser(ctx context.Context, user *User) (*SpamReport, error) {
	spamReport := &SpamReport{}
	has, err := db.GetEngine(ctx).Where("user_id = ?", user.ID).Get(spamReport)
	if has {
		return spamReport, err
	} else {
		return nil, err
	}
}
