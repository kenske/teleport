/*
Copyright 2017-2021 Gravitational, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package services

import (
	"testing"

	"github.com/gravitational/teleport/api/types"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
)

func TestUnmarshal(t *testing.T) {
	t.Parallel()

	data := []byte(`{"kind": "github",
"version": "v3",
"metadata": {
  "name": "github"
},
"spec": {
  "client_id": "aaa",
  "client_secret": "bbb",
  "display": "Github",
  "redirect_url": "https://localhost:3080/v1/webapi/github/callback",
  "teams_to_logins": [{
    "organization": "gravitational",
    "team": "admins",
    "logins": ["admin"]
  }]
}}`)
	connector, err := UnmarshalGithubConnector(data)
	require.NoError(t, err)
	expected, err := types.NewGithubConnector("github", types.GithubConnectorSpecV3{
		ClientID:     "aaa",
		ClientSecret: "bbb",
		RedirectURL:  "https://localhost:3080/v1/webapi/github/callback",
		Display:      "Github",
		TeamsToRoles: []types.TeamRolesMapping{
			{
				Organization: "gravitational",
				Team:         "admins",
				Roles:        []string{"admin"},
			},
		},
	})
	require.NoError(t, err)
	require.Empty(t, cmp.Diff(expected, connector))
}

func TestMapClaims(t *testing.T) {
	t.Parallel()

	connector, err := types.NewGithubConnector("github", types.GithubConnectorSpecV3{
		ClientID:     "aaa",
		ClientSecret: "bbb",
		RedirectURL:  "https://localhost:3080/v1/webapi/github/callback",
		Display:      "Github",
		TeamsToRoles: []types.TeamRolesMapping{
			{
				Organization: "gravitational",
				Team:         "admins",
				Roles:        []string{"system"},
			},
			{
				Organization: "gravitational",
				Team:         "devs",
				Roles:        []string{"dev"},
			},
		},
	})
	require.NoError(t, err)

	roles, kubeGroups, kubeUsers := connector.MapClaims(types.GithubClaims{
		OrganizationToTeams: map[string][]string{
			"gravitational": {"admins"},
		},
	})
	require.Empty(t, cmp.Diff(roles, []string{"system"}))
	require.Empty(t, kubeGroups)
	require.Empty(t, kubeUsers)

	roles, kubeGroups, kubeUsers = connector.MapClaims(types.GithubClaims{
		OrganizationToTeams: map[string][]string{
			"gravitational": {"devs"},
		},
	})

	require.Empty(t, cmp.Diff(roles, []string{"dev"}))
	require.Empty(t, kubeGroups)
	require.Empty(t, kubeUsers)

	roles, kubeGroups, kubeUsers = connector.MapClaims(types.GithubClaims{
		OrganizationToTeams: map[string][]string{
			"gravitational": {"admins", "devs"},
		},
	})
	require.Empty(t, cmp.Diff(roles, []string{"system", "dev"}))
	require.Empty(t, kubeGroups)
	require.Empty(t, kubeUsers)
}
