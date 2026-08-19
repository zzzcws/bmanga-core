import { Component, type ErrorInfo, type ReactNode } from "react";

import { useI18n, type LocalizedText } from "../i18n";

const copy = {
  interrupted: {
    "zh-CN": "SECTION INTERRUPTED",
    en: "SECTION INTERRUPTED",
    ja: "セクションの読み込みが中断されました",
  },
  reload: {
    "zh-CN": "重新加载页面",
    en: "Reload page",
    ja: "ページを再読み込み",
  },
} satisfies Record<string, LocalizedText>;

interface AsyncRegionBoundaryProps {
  children: ReactNode;
  title: string;
  copy: string;
  resetKey: string;
}

interface AsyncRegionBoundaryInnerProps extends AsyncRegionBoundaryProps {
  interruptedLabel: string;
  reloadLabel: string;
}

interface AsyncRegionBoundaryState {
  failed: boolean;
}

class AsyncRegionBoundaryInner extends Component<AsyncRegionBoundaryInnerProps, AsyncRegionBoundaryState> {
  state: AsyncRegionBoundaryState = { failed: false };

  static getDerivedStateFromError(): AsyncRegionBoundaryState {
    return { failed: true };
  }

  componentDidUpdate(previous: AsyncRegionBoundaryInnerProps): void {
    if (this.state.failed && previous.resetKey !== this.props.resetKey) {
      this.setState({ failed: false });
    }
  }

  componentDidCatch(_error: Error, _info: ErrorInfo): void {
    // The region keeps the surrounding application available. A reload is the
    // only reliable retry after a deployment replaces a hashed lazy chunk.
  }

  render(): ReactNode {
    if (!this.state.failed) return this.props.children;
    return (
      <section className="async-region-error" role="alert">
        <span>{this.props.interruptedLabel}</span>
        <h3>{this.props.title}</h3>
        <p>{this.props.copy}</p>
        <button type="button" onClick={() => window.location.reload()}>{this.props.reloadLabel}</button>
      </section>
    );
  }
}

export function AsyncRegionBoundary(props: AsyncRegionBoundaryProps) {
  const { text } = useI18n();
  return (
    <AsyncRegionBoundaryInner
      {...props}
      interruptedLabel={text(copy.interrupted)}
      reloadLabel={text(copy.reload)}
    />
  );
}
