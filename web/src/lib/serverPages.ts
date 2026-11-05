// The addresses a navigation must take to the server rather than to the
// app the service worker keeps (its navigateFallback):
// - images an agent, a plan or a review showed, opened in a tab of their own;
// - /oauth/callback, where the browser comes back from an integration's
//   consent page (Burnlite) with the code the server exchanges.
export const SERVER_PAGES: RegExp[] = [
  /^\/agent-ui-images\//,
  /^\/plan-images\//,
  /^\/review-images\//,
  /^\/oauth\//,
]
