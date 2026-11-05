import { mutationOptions } from "@tanstack/react-query";

import { withRetryAfter } from "@/api/errors";
import { login, type LoginInputBody } from "@/api/generated";

/**
 * The mutation of a login. Unlike `loginMutation()` of the generated client,
 * its error keeps the wait that a 429 `rate_limited` gives in its Retry-After
 * header, which the generated client leaves out of the errors it throws.
 */
export function loginMutationOptions() {
  return mutationOptions({
    mutationFn: async (body: LoginInputBody) => {
      const { data, error, response } = await login({ body });
      if (error !== undefined) {
        throw withRetryAfter(error, response);
      }
      return data;
    },
  });
}
