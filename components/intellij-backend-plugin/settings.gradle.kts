// Copyright (c) 2021 Gitpod GmbH. All rights reserved.
// Licensed under the GNU Affero General Public License (AGPL).
// See License-AGPL.txt in the project root for license information.

rootProject.name = "gitpod-backend-plugin"

include(":supervisor-api")
project(":supervisor-api").projectDir = File("../supervisor-api/java/")

include(":gitpod-protocol")
project(":gitpod-protocol").projectDir = File("../gitpod-protocol/java/")
