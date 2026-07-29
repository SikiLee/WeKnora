export interface LatestOperationGuard {
  begin(): number;
  isCurrent(operation: number): boolean;
  invalidate(): void;
}

export function createLatestOperationGuard(): LatestOperationGuard {
  let current = 0;
  return {
    begin: () => ++current,
    isCurrent: (operation) => operation === current,
    invalidate: () => {
      current += 1;
    },
  };
}
