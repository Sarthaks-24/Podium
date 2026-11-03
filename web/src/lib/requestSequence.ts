/** A guard for async reads where only the newest result may be applied. */
export class RequestSequence {
  private value = 0;

  next(): number {
    return ++this.value;
  }

  isCurrent(request: number): boolean {
    return request === this.value;
  }
}
