/*
 * Licensed to the Apache Software Foundation (ASF) under one
 * or more contributor license agreements.  See the NOTICE file
 * distributed with this work for additional information
 * regarding copyright ownership.  The ASF licenses this file
 * to you under the Apache License, Version 2.0 (the
 * "License"); you may not use this file except in compliance
 * with the License.  You may obtain a copy of the License at
 *
 *   http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

package controller

import (
	"net/http"

	"github.com/apache/answer/internal/base/middleware"
	"github.com/gin-gonic/gin"
	"github.com/segmentfault/pacman/log"
)

func (cc *ConnectorController) ResolveEntraUsers(ctx *gin.Context) {
	ctx.Header("Cache-Control", "no-store")
	var request struct {
		Objects []string `json:"object_ids"`
	}
	if err := ctx.ShouldBindJSON(&request); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "Invalid attribution request"})
		return
	}
	users, err := cc.userExternalService.ResolveEntraUsers(ctx,
		middleware.GetLoginUserIDFromContext(ctx), request.Objects)
	if err != nil {
		// Neither account details nor identity existence are exposed on refusal.
		log.Warnf("attribution lookup unavailable: %v", err)
		ctx.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": "Attribution lookup unavailable"})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"code": 200, "data": users})
}
