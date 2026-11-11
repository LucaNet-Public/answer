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

import React, {
  useEffect,
  useId,
  useLayoutEffect,
  useRef,
  useState,
  FC,
} from 'react';
import { useTranslation } from 'react-i18next';

import { userSearchByName } from '@/services';
import type { PageUser } from '@/common/interface';

import './index.scss';

interface IProps {
  children: React.ReactElement;
  pageUsers: PageUser[];
  onSelected: (val: string, username: string) => void;
}

const MAX_RECORDS = 5;
type Mention = { start: number; end: number; query: string };
type Search = {
  query: string;
  users: PageUser[];
  status: 'loading' | 'done' | 'error';
};

const Mentions: FC<IProps> = ({ children, pageUsers = [], onSelected }) => {
  const { t } = useTranslation();
  const listId = useId();
  const wrapperRef = useRef<HTMLDivElement>(null);
  const selectionRef = useRef<number | null>(null);
  const dismissedRef = useRef<Mention | null>(null);
  const [mention, setMention] = useState<Mention | null>(null);
  const [search, setSearch] = useState<Search | null>(null);
  const [cursor, setCursor] = useState(0);
  const query = mention?.query;

  useEffect(() => {
    if (!query) return undefined;
    let cancelled = false;
    setSearch({ query, users: [], status: 'loading' });
    const timer = setTimeout(() => {
      userSearchByName(query, { ignoreError: '50X' }).then(
        (result) => {
          if (!cancelled) {
            setSearch({
              query,
              status: 'done',
              users: result.map((user) => ({
                displayName: user.display_name,
                userName: user.username,
              })),
            });
          }
        },
        () => {
          if (!cancelled) setSearch({ query, users: [], status: 'error' });
        },
      );
    }, 200);
    return () => {
      cancelled = true;
      clearTimeout(timer);
    };
  }, [query]);

  const updateMention = (target: EventTarget | null) => {
    if (!(target instanceof HTMLTextAreaElement)) return;
    const { value, selectionStart, selectionEnd } = target;
    // A mention begins at a word boundary, never inside an email address.
    const match = value
      .slice(0, selectionStart)
      .match(/(?:^|[\s(])@([^\s@]*)$/);
    if (!match || selectionStart !== selectionEnd) {
      dismissedRef.current = null;
      setMention(null);
      return;
    }
    const next = {
      start: selectionStart - match[1].length - 1,
      end: selectionStart,
      query: match[1],
    };
    const dismissed = dismissedRef.current;
    if (
      dismissed?.start === next.start &&
      dismissed.end === next.end &&
      dismissed.query === next.query
    )
      return;
    dismissedRef.current = null;
    setMention((previous) => {
      if (
        previous?.start === next.start &&
        previous.end === next.end &&
        previous.query === next.query
      )
        return previous;
      return next;
    });
    setCursor(0);
  };

  const normalizedQuery = (query || '').toLocaleLowerCase();
  const localUsers = pageUsers.filter(
    (user) =>
      user.userName &&
      (!query ||
        user.userName.toLocaleLowerCase().startsWith(normalizedQuery) ||
        user.displayName?.toLocaleLowerCase().startsWith(normalizedQuery)),
  );
  const remoteUsers = search && search.query === query ? search.users : [];
  const users = [...localUsers, ...remoteUsers]
    .filter(
      (user, index, all) =>
        user.userName &&
        all.findIndex((other) => other.userName === user.userName) === index,
    )
    .slice(0, MAX_RECORDS);
  const activeIndex = Math.min(cursor, Math.max(users.length - 1, 0));

  const selectUser = (user: PageUser) => {
    const textarea = wrapperRef.current?.querySelector('textarea');
    if (!textarea || !mention) return;
    const text = `@${user.userName} `;
    selectionRef.current = mention.start + text.length;
    onSelected(
      `${textarea.value.slice(0, mention.start)}${text}${textarea.value.slice(mention.end)}`,
      user.userName,
    );
    setMention(null);
  };

  // The parent owns the draft. Restore focus/caret after its controlled update.
  useLayoutEffect(() => {
    const textarea = wrapperRef.current?.querySelector('textarea');
    if (textarea && selectionRef.current !== null) {
      textarea.focus();
      textarea.setSelectionRange(selectionRef.current, selectionRef.current);
      selectionRef.current = null;
    }
  });

  const handleKeyDown = (event: React.KeyboardEvent) => {
    if (!mention || event.nativeEvent.isComposing) return;
    if (event.key === 'Escape') {
      event.preventDefault();
      event.stopPropagation();
      dismissedRef.current = mention;
      setMention(null);
    } else if (
      users.length &&
      ['ArrowUp', 'ArrowDown', 'Enter'].includes(event.key)
    ) {
      event.preventDefault();
      event.stopPropagation();
      if (event.key === 'Enter') selectUser(users[activeIndex]);
      else
        setCursor(
          Math.max(
            0,
            Math.min(
              users.length - 1,
              activeIndex + (event.key === 'ArrowDown' ? 1 : -1),
            ),
          ),
        );
    }
  };

  const status = !query
    ? t('mentions.search_hint', 'Type a name to search people.')
    : search?.query !== query || search.status === 'loading'
      ? t('mentions.loading', 'Searching people…')
      : search.status === 'error'
        ? t(
            'mentions.error',
            'Could not search people. Keep typing to try again.',
          )
        : users.length === 0
          ? t('mentions.empty', 'No people found.')
          : '';

  return (
    <div
      ref={wrapperRef}
      className="mentions-wrap position-relative"
      onInput={(event) => updateMention(event.target)}
      onSelect={(event) => updateMention(event.target)}
      onClick={(event) => updateMention(event.target)}
      onKeyDown={handleKeyDown}
      onBlur={(event) => {
        if (!event.currentTarget.contains(event.relatedTarget as Node | null))
          setMention(null);
      }}>
      {React.cloneElement(children, {
        'aria-autocomplete': 'list',
        'aria-expanded': Boolean(mention),
        'aria-controls': mention ? listId : undefined,
        'aria-activedescendant':
          mention && users.length ? `${listId}-${activeIndex}` : undefined,
      })}
      {mention && (
        <div className="dropdown-menu show w-100 mentions-menu">
          <div
            id={listId}
            role="listbox"
            aria-label={t('mentions.label', 'People to mention')}>
            {users.map((user, index) => (
              <button
                key={user.userName}
                id={`${listId}-${index}`}
                type="button"
                role="option"
                tabIndex={-1}
                aria-selected={activeIndex === index}
                className={`dropdown-item text-wrap ${activeIndex === index ? 'bg-gray-200' : ''}`}
                onPointerDown={(event) => event.preventDefault()}
                onClick={() => selectUser(user)}>
                <span className="link-dark me-2">{user.displayName}</span>
                <small className="link-secondary">@{user.userName}</small>
              </button>
            ))}
          </div>
          {status && (
            <div role="status" className="px-3 py-2 small text-secondary">
              {status}
            </div>
          )}
        </div>
      )}
    </div>
  );
};

export default Mentions;
