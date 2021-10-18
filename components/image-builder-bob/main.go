// Copyright (c) 2021 Gitpod GmbH. All rights reserved.
// Licensed under the GNU Affero General Public License (AGPL).
// See License-AGPL.txt in the project root for license information.

package main

import (
	"time"

	"github.com/gitpod-io/gitpod/image-builder/bob/cmd"
)

func main() {
	defer func() {
		time.Sleep(2 * time.Minute)
	}()

	cmd.Execute()
}
