// Copyright 2021 Gravitational, Inc
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package handler

import (
	"context"
	"sort"

	api "github.com/gravitational/teleport/lib/teleterm/api/protogen/golang/v1"
	"github.com/gravitational/teleport/lib/teleterm/clusters"

	"github.com/gravitational/trace"
)

// ListDatabases lists databases
func (s *Handler) ListDatabases(ctx context.Context, req *api.ListDatabasesRequest) (*api.ListDatabasesResponse, error) {
	cluster, err := s.DaemonService.ResolveCluster(req.ClusterUri)
	if err != nil {
		return nil, trace.Wrap(err)
	}

	dbs, err := cluster.GetDatabases(ctx)
	if err != nil {
		return nil, trace.Wrap(err)
	}

	response := &api.ListDatabasesResponse{}
	for _, db := range dbs {
		response.Databases = append(response.Databases, newAPIDatabase(db))
	}

	return response, nil
}

// ListDatabaseUsers is used to list database user suggestions when the user is attempting to
// establish a connection to a database through Teleterm.
//
// The list is based on whatever we can deduce from the role set, so it's similar to the behavior of
// `tsh db ls -v`, with the exception that Teleterm is interested only in the allowed usernames.
func (s *Handler) ListDatabaseUsers(ctx context.Context, req *api.ListDatabaseUsersRequest) (*api.ListDatabaseUsersResponse, error) {
	cluster, err := s.DaemonService.ResolveCluster(req.DbUri)
	if err != nil {
		return nil, trace.Wrap(err)
	}

	dbUsers, err := cluster.GetAllowedDatabaseUsers(ctx, req.DbUri)
	if err != nil {
		return nil, trace.Wrap(err)
	}

	return &api.ListDatabaseUsersResponse{
		Users: dbUsers,
	}, nil
}

func newAPIDatabase(db clusters.Database) *api.Database {
	apiLabels := APILabels{}
	for name, value := range db.GetAllLabels() {
		apiLabels = append(apiLabels, &api.Label{
			Name:  name,
			Value: value,
		})
	}

	sort.Sort(apiLabels)

	return &api.Database{
		Uri:      db.URI.String(),
		Name:     db.GetName(),
		Desc:     db.GetDescription(),
		Protocol: db.GetProtocol(),
		Type:     db.GetType(),
		Labels:   apiLabels,
	}
}
