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
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/apache/answer/internal/entity"
	_ "github.com/lib/pq"
	"github.com/segmentfault/pacman/contrib/cache/memory"
	_ "modernc.org/sqlite"
	"xorm.io/xorm"
)

func TestKVCompareAndSwapAcrossOperators(t *testing.T) {
	driver, dsn := "sqlite", filepath.Join(t.TempDir(), "grants.db")
	if value := os.Getenv("ANSWER_MCP_TEST_POSTGRES_DSN"); value != "" {
		driver, dsn = "postgres", value
	}
	if os.Getenv("ANSWER_MCP_TEST_REQUIRE_POSTGRES") == "1" && driver != "postgres" {
		t.Fatal("PostgreSQL regression requires its isolated test database")
	}
	t.Logf("atomic grant regression on %s", driver)
	db, err := xorm.NewEngine(driver, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if driver == "sqlite" {
		db.SetMaxOpenConns(1)
	} else {
		db.SetMaxOpenConns(20)
	}
	if err = db.Sync2(new(entity.PluginKVStorage)); err != nil {
		t.Fatal(err)
	}
	first, second := NewKVOperator(db, memory.NewCache(), "mcp_test"), NewKVOperator(db, memory.NewCache(), "mcp_test")
	first.Option(WithCacheTTL(-1))
	second.Option(WithCacheTTL(-1))
	ctx := context.Background()
	p := KVParams{Group: "codes", Key: "hash", Value: "available"}
	if err = first.Set(ctx, p); err != nil {
		t.Fatal(err)
	}
	// Warm a separate replica cache: the control must remain stale after CAS.
	cached := NewKVOperator(db, memory.NewCache(), "mcp_test")
	for _, op := range []*KVOperator{first, second, cached} {
		if value, err := op.Get(ctx, p); err != nil || value != "available" {
			t.Fatalf("initial value: %q %v", value, err)
		}
	}
	var wins atomic.Int32
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Go(func() {
			op := first
			if i%2 == 1 {
				op = second
			}
			next := p
			next.Value = "consumed"
			ok, err := op.CompareAndSwap(ctx, next, "available")
			if err != nil {
				t.Error(err)
			}
			if ok {
				wins.Add(1)
			}
		})
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("%d redemptions succeeded", wins.Load())
	}
	if value, err := second.Get(ctx, p); err != nil || value != "consumed" {
		t.Fatalf("stale replica read: %q %v", value, err)
	}
	if value, err := cached.Get(ctx, p); err != nil || value != "available" {
		t.Fatalf("cached control did not retain old value: %q %v", value, err)
	}
	if ok, err := first.CompareAndSwap(ctx, KVParams{Group: "codes", Key: "missing", Value: "x"}, "missing"); err != nil || ok {
		t.Fatalf("CAS created missing grant: %v %v", ok, err)
	}
}
