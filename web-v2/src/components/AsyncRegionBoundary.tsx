import { Component, type ErrorInfo, type ReactNode } from "react";

interface AsyncRegionBoundaryProps {
  children: ReactNode;
  title: string;
  copy: string;
  resetKey: string;
}

interface AsyncRegionBoundaryState {
  failed: boolean;
}

export class AsyncRegionBoundary extends Component<AsyncRegionBoundaryProps, AsyncRegionBoundaryState> {
  state: AsyncRegionBoundaryState = { failed: false };

  static getDerivedStateFromError(): AsyncRegionBoundaryState {
    return { failed: true };
  }

  componentDidUpdate(previous: AsyncRegionBoundaryProps): void {
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
        <span>SECTION INTERRUPTED</span>
        <h3>{this.props.title}</h3>
        <p>{this.props.copy}</p>
        <button type="button" onClick={() => window.location.reload()}>重新加载页面</button>
      </section>
    );
  }
}
