export function mountOperator(
  host: HTMLElement,
  options: {
    onAssistantMount: (element: HTMLElement | null) => void;
  },
): () => void;
