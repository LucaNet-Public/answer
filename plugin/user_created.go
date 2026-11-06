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

package plugin

import "context"

// NewUser observes a successfully committed account. Batch inserts can leave
// userID empty; UserIDByEmail resolves its generated ID. Plugins own their
// persistence and error handling; this notification cannot reject registration.
type NewUser interface {
	Base
	UserCreated(ctx context.Context, userID, email string)
}

var callNewUser, registerNewUser = MakePlugin[NewUser](false)

func NotifyUserCreated(ctx context.Context, userID, email string) {
	_ = callNewUser(func(p NewUser) error {
		p.UserCreated(ctx, userID, email)
		return nil
	})
}
