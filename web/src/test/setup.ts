// Test setup: jest-dom matchers, cleanup after each test, and the parts of
// the browser jsdom leaves out.
import "@testing-library/jest-dom/vitest";
import { cleanup, configure } from "@testing-library/react";
import { afterEach } from "vitest";

afterEach(() => cleanup());

// findBy and waitFor give up after 5 s, not 1 s: on a loaded runner a heavy
// page can take longer than a second to render what they wait for. A test
// that passes never waits that long.
configure({ asyncUtilTimeout: 5000 });

if (typeof HTMLDialogElement !== "undefined" && !HTMLDialogElement.prototype.showModal) {
  HTMLDialogElement.prototype.showModal = function showModal(this: HTMLDialogElement) {
    this.setAttribute("open", "");
  };
  HTMLDialogElement.prototype.close = function close(this: HTMLDialogElement) {
    this.removeAttribute("open");
  };
}

// jsdom has no viewport scrolling; modal tests can spy on restoration calls.
window.scrollTo = () => {};
