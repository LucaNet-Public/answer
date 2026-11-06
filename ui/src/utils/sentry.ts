import * as Sentry from '@sentry/react';

import { loggedUserInfoStore } from '@/stores';

// Lucanet: browser errors and page loads go to Lucanet's Sentry, project
// answer-frontend. The DSN is baked in at build time; it can only submit
// events, and the browser has to read it anyway. Production runs staging's
// image unchanged, so the environment cannot be baked in as well: it is read
// from the address the page was served from, and any other address -- a local
// run, a preview -- reports nothing. Source maps are matched by the debug IDs
// the build injects (config-overrides.js), so no release is needed for them.
// See docs/sentry.md in the Lucanet repository.
export function startSentry() {
  const dsn = process.env.REACT_APP_SENTRY_DSN;
  const hostname = window.location.hostname;
  const environment = hostname && hostname === process.env.REACT_APP_SENTRY_PRODUCTION_HOST
    ? 'production'
    : hostname && hostname === process.env.REACT_APP_SENTRY_STAGING_HOST
      ? 'staging'
      : undefined;
  if (!dsn || !environment) {
    return;
  }
  Sentry.init({
    dsn,
    environment,
    // Page loads and navigations, joined to the server's traces through the
    // headers the SDK adds to same-origin API calls. The rates match the
    // server's.
    integrations: [
      Sentry.browserTracingIntegration(),
      Sentry.replayIntegration({
        // Questions, answers and AI chat must stay masked in recordings.
        maskAllText: true,
        maskAllInputs: true,
        blockAllMedia: true,
        networkCaptureBodies: false,
      }),
    ],
    // Record 1% of sessions from the start. For the rest, keep a rolling
    // buffer and upload it whenever a browser error is reported to Sentry.
    replaysSessionSampleRate: 0.01,
    replaysOnErrorSampleRate: 1.0,
    tracesSampleRate: environment === 'production' ? 0.1 : 1,
    // Request and response bodies are questions, answers and AI chat
    // replies, and cookies carry the session. An event names the user by ID
    // only, set below.
    dataCollection: {
      userInfo: false,
      cookies: false,
      httpBodies: [],
      genAI: { inputs: false, outputs: false },
      stackFrameVariables: false,
    },
  });

  const setUser = (id?: string) => Sentry.setUser(id ? { id } : null);
  setUser(loggedUserInfoStore.getState().user.id);
  loggedUserInfoStore.subscribe((state) => setUser(state.user.id));
}
