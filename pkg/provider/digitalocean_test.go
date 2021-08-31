// SPDX-License-Identifier: Apache-2.0
//
// Copyright 2021 Digital Ocean, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package provider

import (
	"context"
	"net"
	"testing"

	"github.com/digitalocean/godo"

	"github.com/digitalocean/flipop/pkg/log"

	"github.com/digitalocean/flipop/pkg/provider/mock_godo"
	"github.com/golang/mock/gomock"

	"github.com/stretchr/testify/require"
)

func TestDigitalOceanEnsureDNSARecordSet(t *testing.T) {
	tcs := []struct {
		name         string
		curIPs       []string
		setIPs       []string
		curTTL       int
		totalRecords int

		expectUpdates map[int]string // id -> ip
		expectCreates []string       // ips
		expectDeletes []int          // id
	}{
		{
			name:         "already set",
			curIPs:       []string{"10.0.1.0", "10.0.1.255"},
			setIPs:       []string{"10.0.1.0", "10.0.1.255"},
			curTTL:       30,
			totalRecords: 513,
		},
		{
			name:          "update",
			curIPs:        []string{"10.0.1.0", "10.0.1.255"},
			setIPs:        []string{"10.0.1.0", "10.0.0.1"},
			curTTL:        30,
			totalRecords:  513,
			expectUpdates: map[int]string{511: "10.0.0.1"},
		},
		{
			name:          "create",
			curIPs:        []string{"10.0.1.0", "10.0.1.255"},
			setIPs:        []string{"10.0.1.0", "10.0.1.255", "10.0.1.1"},
			curTTL:        30,
			totalRecords:  513,
			expectCreates: []string{"10.0.1.1"},
		},
		{
			name:          "delete",
			curIPs:        []string{"10.0.1.0", "10.0.1.255"},
			setIPs:        []string{"10.0.1.0"},
			curTTL:        30,
			totalRecords:  513,
			expectDeletes: []int{511},
		},
	}
	for _, tc := range tcs {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			domainsService := mock_godo.NewMockDomainsService(ctrl)

			curIPSet := make(map[string]struct{})
			for _, ip := range tc.curIPs {
				curIPSet[ip] = struct{}{}
			}

			var page int
			for i := 0; i < tc.totalRecords; {
				page++
				records := make([]godo.DomainRecord, 0, dnsRecordsPerPage)
				for j := 0; j < dnsRecordsPerPage && i < tc.totalRecords; {
					// 10.0.0.0, 10.0.0.1, 10.0.0.2, 10.0.0.3...
					ip := net.IPv4(10, byte(i>>16), byte(i>>8), byte(i)).String()
					name := "no-match"
					if _, ok := curIPSet[ip]; ok {
						name = "match"
					}
					records = append(records, godo.DomainRecord{
						ID:   i,
						Name: name,
						Type: dnsRecordTypeA,
						Data: ip,
						TTL:  tc.curTTL,
					})
					i++
					j++
				}
				nextPage := ""
				if i < tc.totalRecords {
					nextPage = "more pages" // value doesn't actually matter.
				}
				domainsService.EXPECT().Records(gomock.Any(), "zone", &godo.ListOptions{
					PerPage: dnsRecordsPerPage,
					Page:    page,
				}).Return(records, &godo.Response{Links: &godo.Links{Pages: &godo.Pages{Next: nextPage}}}, nil)
			}

			for id, ip := range tc.expectUpdates {
				domainsService.EXPECT().EditRecord(gomock.Any(), "zone", id, &godo.DomainRecordEditRequest{
					Name: "match",
					Type: dnsRecordTypeA,
					Data: ip,
					TTL:  tc.curTTL,
				})
			}

			for _, ip := range tc.expectCreates {
				domainsService.EXPECT().CreateRecord(gomock.Any(), "zone", &godo.DomainRecordEditRequest{
					Name: "match",
					Type: dnsRecordTypeA,
					Data: ip,
					TTL:  tc.curTTL,
				})
			}

			for _, id := range tc.expectDeletes {
				domainsService.EXPECT().DeleteRecord(gomock.Any(), "zone", id)
			}

			do := &digitalOcean{
				domainsService: domainsService,
				log:            log.NewTestLogger(t),
			}
			err := do.EnsureDNSARecordSet(ctx, "zone", "match", tc.setIPs, 30)
			require.NoError(t, err)
		})
	}
}
