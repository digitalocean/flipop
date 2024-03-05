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
	"crypto/tls"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cloudflare/cloudflare-go"
	"github.com/digitalocean/flipop/pkg/log"

	"github.com/stretchr/testify/require"
)

func TestCloudflareEnsureDNSARecordSet(t *testing.T) {
	const (
		name = "name"
		zone = "ZONE"
	)
	tcs := []struct {
		name string
		list map[string]cloudflare.DNSRecord

		setTTL int
		setIPs []string

		expectUpdates map[string]*cloudflare.DNSRecord
		expectCreates map[string]*cloudflare.DNSRecord
		expectDeletes []string
	}{
		{
			name: "already set",
			list: map[string]cloudflare.DNSRecord{
				"1": {Name: name, ID: "1", Content: "10.0.1.0", TTL: 30, Type: dnsRecordTypeA},
				"2": {Name: name, ID: "2", Content: "10.0.1.255", TTL: 30, Type: dnsRecordTypeA},
			},
			setIPs: []string{"10.0.1.0", "10.0.1.255"},
			setTTL: 30,
		},
		{
			name: "update",
			list: map[string]cloudflare.DNSRecord{
				"1": {Name: name, ID: "1", Content: "10.0.1.0", TTL: 30, Type: dnsRecordTypeA},
				"2": {Name: name, ID: "2", Content: "10.0.1.255", TTL: 30, Type: dnsRecordTypeA},
			},
			setIPs: []string{"10.0.1.0", "10.0.0.1"},
			setTTL: 30,
			expectUpdates: map[string]*cloudflare.DNSRecord{
				"2": {Name: name, Content: "10.0.0.1", TTL: 30, Type: dnsRecordTypeA},
			},
		},
		{
			name: "create",
			list: map[string]cloudflare.DNSRecord{
				"1": {Name: name, ID: "1", Content: "10.0.1.0", TTL: 30, Type: dnsRecordTypeA},
				"2": {Name: name, ID: "2", Content: "10.0.0.1", TTL: 30, Type: dnsRecordTypeA},
			},
			setIPs: []string{"10.0.1.0", "10.0.0.1", "10.0.0.2"},
			setTTL: 30,
			expectCreates: map[string]*cloudflare.DNSRecord{
				"10.0.0.2": {Name: name, Content: "10.0.0.2", TTL: 30, Type: dnsRecordTypeA},
			},
		},
		{
			name: "delete",
			list: map[string]cloudflare.DNSRecord{
				"1": {Name: name, ID: "1", Content: "10.0.1.0", TTL: 30, Type: dnsRecordTypeA},
				"2": {Name: name, ID: "2", Content: "10.0.0.1", TTL: 30, Type: dnsRecordTypeA},
			},
			setIPs:        []string{"10.0.1.0"},
			setTTL:        30,
			expectDeletes: []string{"2"},
		},
	}
	for _, tc := range tcs {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			expectDeletes := make(map[string]struct{})
			for _, id := range tc.expectDeletes {
				expectDeletes[id] = struct{}{}
			}

			ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var out interface{} = make(map[string]interface{})
				recordPrefix := "/client/v4/zones/" + zone + "/dns_records/"
				if strings.HasPrefix(r.URL.Path, recordPrefix) {
					id := strings.TrimPrefix(r.URL.Path, recordPrefix)
					require.NotEmpty(t, id)
					switch r.Method {
					case http.MethodGet:
						out = tc.list[id]
					case http.MethodPatch:
						record := &cloudflare.DNSRecord{}
						err := json.NewDecoder(r.Body).Decode(record)
						require.NoError(t, err)
						require.Equal(t, tc.expectUpdates[id], record)
						delete(tc.expectUpdates, id)
					case http.MethodDelete:
						require.Contains(t, tc.expectDeletes, id)
						delete(expectDeletes, id)
					default:
						t.Fatalf("unexpected method %q %s", r.Method, r.RequestURI)
					}
				} else {
					switch r.Method {
					case http.MethodGet:
						list := &cloudflare.DNSListResponse{}
						for _, r := range tc.list {
							list.Result = append(list.Result, r)
						}
						out = list
					case http.MethodPost:
						record := &cloudflare.DNSRecord{}
						err := json.NewDecoder(r.Body).Decode(record)
						require.NoError(t, err)
						require.NotEmpty(t, record.Content)
						require.Equal(t, tc.expectCreates[record.Content], record)
						delete(tc.expectCreates, record.Content)
					default:
						t.Fatalf("unexpected method %q %s", r.Method, r.RequestURI)
					}
				}
				b, err := json.Marshal(out)
				require.NoError(t, err)
				_, err = w.Write(b)
				require.NoError(t, err)
			}))
			defer ts.Close()

			client := ts.Client()
			token := "test123"

			// This might be a little heavy handed, but we can't override the cloudflare api URL in
			// the cloudflare package, so we override the dialer to always connect to the server.
			transport := client.Transport.(*http.Transport).Clone()
			transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
			transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
				return net.Dial(network, ts.Listener.Addr().String())
			}
			client.Transport = transport

			api, err := cloudflare.NewWithAPIToken(token, cloudflare.HTTPClient(client))
			require.NoError(t, err)
			cf := &cloudflareDNS{
				api: api,
				log: log.NewTestLogger(t),
			}
			err = cf.EnsureDNSARecordSet(ctx, zone, name, tc.setIPs, tc.setTTL)
			require.NoError(t, err)
			require.Empty(t, tc.expectUpdates)
			require.Empty(t, tc.expectCreates)
			require.Empty(t, expectDeletes)
		})
	}
}
