/** An HTTP failure carrying enough to decide what the screen should say. The
 * status is kept because 401 and 403 are different conversations with the
 * operator: one is "sign in", the other is "you cannot see this". */
export class ApiError extends Error {
  readonly status: number;
  readonly url: string;

  constructor(status: number, url: string, message: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.url = url;
  }

  get unauthenticated(): boolean {
    return this.status === 401;
  }

  get forbidden(): boolean {
    return this.status === 403;
  }
}
