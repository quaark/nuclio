/*
Copyright 2017 The Nuclio Authors.

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

package opa

import (
	"fmt"
	"net/http"
	"strings"
)

type PermissionRequestInput struct {
	Resource string   `json:"resource,omitempty"`
	Action   string   `json:"action,omitempty"`
	Ids      []string `json:"ids,omitempty"`
}

type PermissionRequest struct {
	Input PermissionRequestInput `json:"input,omitempty"`
}

type PermissionResponse struct {
	Result bool `json:"result,omitempty"`
}

const (
	UserIDHeader       string = "x-user-id"
	UserGroupIdsHeader string = "x-user-group-ids"
)

type Action string

const (
	ActionRead   Action = "read"
	ActionCreate Action = "create"
	ActionUpdate Action = "update"
	ActionDelete Action = "delete"
)

func GetUserAndGroupIdsFromHeaders(request *http.Request) []string {
	var ids []string

	userID := request.Header.Get(UserIDHeader)
	userGroupIdsStr := request.Header.Get(UserGroupIdsHeader)

	if userID != "" {
		ids = append(ids, userID)
	}

	if userGroupIdsStr != "" {
		ids = append(ids, strings.Split(userGroupIdsStr, ",")...)
	}

	return ids
}

func GenerateProjectResourceString(projectName string) string {
	return fmt.Sprintf("/projects/%s", projectName)
}

func GenerateFunctionResourceString(projectName, functionName string) string {
	return fmt.Sprintf("/projects/%s/functions/%s", projectName, functionName)
}

func GenerateFunctionEventResourceString(projectName, functionName, functionEventName string) string {
	return fmt.Sprintf("/projects/%s/functions/%s/function-events/%s", projectName, functionName, functionEventName)
}
