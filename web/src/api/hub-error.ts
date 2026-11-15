export class HubError extends Error {
  readonly status: number;
  constructor(status: number, message: string) {
    super(message);
    this.name = "HubError";
    this.status = status;
  }
}
