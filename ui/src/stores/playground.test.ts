import { get } from "svelte/store";
import { describe, expect, it } from "vitest";
import {
  playgroundTabs,
  resetUnknownPlaygroundTab,
  selectedPlaygroundModel,
  selectedPlaygroundTab,
  showModelInPlayground,
  takePlaygroundIntent,
  type PlaygroundTab,
} from "./playground";

// "docs" was the Help tab. It is a page of its own now, so a browser that
// still has it stored would open the Playground on a tab that no longer
// exists: every panel hidden, and a header reading "Playground /".
const removedTab = "docs" as unknown as PlaygroundTab;

describe("selectedPlaygroundTab", () => {
  it("falls back to the first tab when the stored one was removed", () => {
    selectedPlaygroundTab.set(removedTab);
    resetUnknownPlaygroundTab();
    expect(get(selectedPlaygroundTab)).toBe(playgroundTabs[0].id);
  });

  it("leaves a tab that still exists alone", () => {
    selectedPlaygroundTab.set("rerank");
    resetUnknownPlaygroundTab();
    expect(get(selectedPlaygroundTab)).toBe("rerank");
  });

  it("no longer offers Help as a tab", () => {
    expect(playgroundTabs.map((tab) => tab.id)).not.toContain("docs");
    expect(playgroundTabs.map((tab) => tab.label)).not.toContain("Help");
  });
});

describe("showModelInPlayground", () => {
  it("switches to the Chat tab and hands the model over as an intent", () => {
    selectedPlaygroundTab.set("rerank");
    selectedPlaygroundModel.set("previous-model");
    showModelInPlayground("peer-a:model-b");
    expect(get(selectedPlaygroundTab)).toBe("chat");
    // Chat selects the model only after saving what is on screen, so the
    // shared store must not change while the link is clicked.
    expect(get(selectedPlaygroundModel)).toBe("previous-model");
    expect(takePlaygroundIntent()).toBe("peer-a:model-b");
  });

  it("hands the model to the next Chat exactly once", () => {
    takePlaygroundIntent();
    showModelInPlayground("vision-model");
    expect(takePlaygroundIntent()).toBe("vision-model");
    // A reload or a later visit is an ordinary one: history is restored again.
    expect(takePlaygroundIntent()).toBeNull();
  });
});
