import { unexpectedResponseCode } from "@/api/errors";
import type { Problem } from "@/api/generated";
import type { CreateClientConfig } from "@/api/generated/client.gen";

/** The media type of the errors that the Verve API answers. */
const problemMediaType = "application/problem+json";

/**
 * Configures the generated API client: requests go to the origin of the app
 * with its cookies, every request other than GET carries the header that the
 * server requires against cross-site request forgery, and every error the
 * client throws for a response is a problem.
 */
export const createClientConfig: CreateClientConfig = (config) => ({
  ...config,
  baseUrl: window.location.origin,
  credentials: "same-origin",
  fetch: async (input, init) => {
    const request = new Request(input, init);
    if (request.method !== "GET") {
      request.headers.set("Verve-Csrf", "1");
    }
    const response = await fetch(request);
    return response.ok || hasProblemMediaType(response)
      ? response
      : asProblem(response);
  },
});

function hasProblemMediaType(response: Response): boolean {
  const mediaType = response.headers.get("Content-Type")?.split(";")[0];
  return mediaType?.trim() === problemMediaType;
}

/**
 * Replaces an error response that does not come from Verve, such as the error
 * page of a reverse proxy, with a problem carrying its status. The client
 * throws the body of error responses, which would otherwise lose the status.
 */
function asProblem(response: Response): Response {
  void response.body?.cancel();
  const problem: Problem = {
    type: "about:blank",
    title: response.statusText,
    status: response.status,
    code: unexpectedResponseCode,
  };
  return Response.json(problem, {
    status: response.status,
    statusText: response.statusText,
    headers: { "Content-Type": problemMediaType },
  });
}
