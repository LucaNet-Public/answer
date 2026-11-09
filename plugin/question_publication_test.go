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

import (
	"context"
	"testing"
)

type invitationTestProvider struct{ calls int }

func (*invitationTestProvider) Info() Info { return Info{SlugName: "publication_hook_test"} }
func (p *invitationTestProvider) QuestionPublished(context.Context, string) error {
	p.calls++
	return nil
}
func TestQuestionInvitationProviderRespectsEnablement(t *testing.T) {
	p := &invitationTestProvider{}
	registerQuestionPublication(p)
	defer StatusManager.Enable(p.Info().SlugName, false)
	invoke := func() error {
		return NotifyQuestionPublished(context.Background(), "1")
	}
	if err := invoke(); err != nil || p.calls != 0 {
		t.Fatal(p.calls, err)
	}
	StatusManager.Enable(p.Info().SlugName, true)
	if err := invoke(); err != nil || p.calls != 1 {
		t.Fatal(p.calls, err)
	}
	StatusManager.Enable(p.Info().SlugName, false)
	if err := invoke(); err != nil || p.calls != 1 {
		t.Fatal(p.calls, err)
	}
}
