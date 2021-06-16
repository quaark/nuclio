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
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/nuclio/nuclio/pkg/common"

	"github.com/nuclio/errors"
	"github.com/nuclio/logger"
)

type HTTPClient struct {
	logger              logger.Logger
	address             string
	permissionQueryPath string
	requestTimeout      time.Duration
}

func NewHTTPClient(parentLogger logger.Logger,
	address string,
	permissionQueryPath string,
	requestTimeout time.Duration) *HTTPClient {
	newClient := HTTPClient{
		logger:              parentLogger.GetChild("opa"),
		address:             address,
		permissionQueryPath: permissionQueryPath,
		requestTimeout:      requestTimeout,
	}

	return &newClient
}

func (c *HTTPClient) QueryPermissions(resource string, action Action, ids []string) (bool, error) {
	c.logger.DebugWith("Checking permissions in OPA",
		"resource", resource,
		"action", action,
		"ids", ids)

	// send the request
	headers := map[string]string{
		"Content-Type": "application/json",
	}
	request := PermissionRequest{Input: PermissionRequestInput{
		resource,
		string(action),
		ids,
	}}
	requestBody, err := json.Marshal(request)
	if err != nil {
		return false, errors.Wrap(err, "Failed to generate request body")
	}

	responseBody, _, err := common.SendHTTPRequest(http.MethodPost,
		fmt.Sprintf("%s%s", c.address, c.permissionQueryPath),
		requestBody,
		headers,
		[]*http.Cookie{},
		http.StatusOK,
		true,
		c.requestTimeout)
	if err != nil {
		return false, errors.Wrap(err, "Failed to send HTTP request to OPA")
	}

	permissionResponse := PermissionResponse{}
	if err := json.Unmarshal(responseBody, &permissionResponse); err != nil {
		return false, errors.Wrap(err, "Failed to unmarshal response body")
	}

	return permissionResponse.Result, nil
}
