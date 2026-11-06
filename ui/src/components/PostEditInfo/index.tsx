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

import { FC, memo } from 'react';
import { Link } from 'react-router-dom';
import { Trans } from 'react-i18next';

import { UserInfoBase } from '@/common/interface';
import Avatar from '../Avatar';

interface Props {
  author?: UserInfoBase;
  editor?: UserInfoBase;
}

const EditorProfile: FC<{ user: UserInfoBase }> = ({ user }) => {
  const profile = (
    <>
      <Avatar
        avatar={user.avatar}
        size="24px"
        searchStr="s=48"
        className="me-1"
        alt=""
      />
      {user.display_name}
    </>
  );
  return user.username && user.status !== 'deleted' ? (
    <Link
      to={`/users/${user.username}`}
      className="d-inline-flex align-items-center align-middle text-break">
      {profile}
    </Link>
  ) : (
    <span className="d-inline-flex align-items-center align-middle">
      {profile}
    </span>
  );
};

const Index: FC<Props> = ({ author, editor }) => {
  if (!author?.id || !editor?.id || author.id === editor.id) {
    return null;
  }

  return (
    <Trans
      i18nKey="question_detail.edited_by"
      defaults=" by <editor/>"
      components={{ editor: <EditorProfile user={editor} /> }}
    />
  );
};

export default memo(Index);
