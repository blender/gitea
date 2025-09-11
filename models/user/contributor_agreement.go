// Copyright 2025 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

// BLENDER: contributor agreement

package user

import (
	"code.gitea.io/gitea/models/db"
	"code.gitea.io/gitea/modules/timeutil"

	"xorm.io/builder"
)

type ContributorAgreement struct {
	ID          int64              `xorm:"pk autoincr"`
	Slug        string             `xorm:"UNIQUE"`
	CreatedUnix timeutil.TimeStamp `xorm:"created"`
	Content     string             `xorm:"TEXT NOT NULL"`
}

type SignedContributorAgreement struct {
	ID                     int64              `xorm:"pk autoincr"`
	UserID                 int64              `xorm:"UNIQUE(s)"`
	ContributorAgreementID int64              `xorm:"UNIQUE(s)"`
	CreatedUnix            timeutil.TimeStamp `xorm:"created"`
	Comment                string             `xorm:"TEXT DEFAULT NULL"`
}

type FindSignedContributorAgreementsOptions struct {
	db.ListOptions
	UserID int64
}

func (opts *FindSignedContributorAgreementsOptions) ToConds() builder.Cond {
	cond := builder.NewCond()
	if opts.UserID != 0 {
		cond = cond.And(builder.Eq{"user_id": opts.UserID})
	}
	return cond
}

func init() {
	// These tables don't exist in the upstream code.
	// We don't introduce migrations for it to avoid migration id clashes.
	// Gitea will create the tables in the database during startup,
	// so no manual action is required until we start modifying the tables.
	db.RegisterModel(new(ContributorAgreement))
	db.RegisterModel(new(SignedContributorAgreement))
}
