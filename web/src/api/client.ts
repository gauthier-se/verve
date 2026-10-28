import type { CreateClientConfig } from "@/api/generated/client.gen";

/**
 * Configures the generated API client: requests go to the origin of the app
 * with its cookies, and every request other than GET carries the header that
 * the server requires against cross-site request forgery.
 */
export const createClientConfig: CreateClientConfig = (config) => ({
  ...config,
  baseUrl: window.location.origin,
  credentials: "same-origin",
  fetch: (input, init) => {
    const request = new Request(input, init);
    if (request.method !== "GET") {
      request.headers.set("Verve-Csrf", "1");
    }
    return fetch(request);
  },
});
