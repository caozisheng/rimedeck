import { render, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { GitlabMediaProvider, useGitlabMediaUrl } from "./gitlab-media-context";

const mocks = vi.hoisted(() => ({ getGitlabMedia: vi.fn(), getBaseUrl: vi.fn(() => "http://127.0.0.1:18080") }));
vi.mock("@rimedeck/core/api", () => ({ api: mocks }));

function Probe({ src }: { src: string }) {
  const resolved = useGitlabMediaUrl(src);
  return <img src={resolved || undefined} alt="probe" />;
}

describe("GitlabMediaProvider", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.getGitlabMedia.mockResolvedValue(new Blob(["image"], { type: "image/png" }));
    Object.defineProperty(URL, "createObjectURL", {
      configurable: true,
      value: vi.fn(() => "blob:gitlab-image"),
    });
    Object.defineProperty(URL, "revokeObjectURL", {
      configurable: true,
      value: vi.fn(),
    });
  });

	it("loads root-relative GitLab uploads through the authenticated issue proxy", async () => {
		const view = render(
			<GitlabMediaProvider issueId="issue-1" instanceUrl="https://gitlab.example.com">
				<Probe src="/uploads/secret/image.png" />
			</GitlabMediaProvider>,
		);

		await waitFor(() => expect(view.getByRole("img")).toHaveAttribute("src", "blob:gitlab-image"));
		expect(mocks.getGitlabMedia).toHaveBeenCalledWith("issue-1", "/uploads/secret/image.png");
		view.unmount();
		expect(URL.revokeObjectURL).toHaveBeenCalledWith("blob:gitlab-image");
	});

	it("re-proxies a GitLab upload that was absolutized against the local API origin", async () => {
		const view = render(
			<GitlabMediaProvider issueId="issue-1" instanceUrl="https://gitlab.example.com">
				<Probe src="http://127.0.0.1:18080/uploads/7a41b46cb01975ab44293b021979b09f/image.jpg" />
			</GitlabMediaProvider>,
		);

		await waitFor(() => expect(view.getByRole("img")).toHaveAttribute("src", "blob:gitlab-image"));
		expect(mocks.getGitlabMedia).toHaveBeenCalledWith("issue-1", "/uploads/7a41b46cb01975ab44293b021979b09f/image.jpg");
		view.unmount();
	});

	it("re-proxies when the provider only has a GitLab issue absolute URL", async () => {
		const view = render(
			<GitlabMediaProvider issueId="issue-1" instanceUrl="https://gitlab.example.com/group/project/-/issues/42">
				<Probe src="/uploads/legacy/image.jpg" />
			</GitlabMediaProvider>,
		);
		await waitFor(() => expect(view.getByRole("img")).toHaveAttribute("src", "blob:gitlab-image"));
		expect(mocks.getGitlabMedia).toHaveBeenCalledWith("issue-1", "/uploads/legacy/image.jpg");
		view.unmount();
	});

  it("leaves unrelated external images untouched", () => {
    const view = render(
      <GitlabMediaProvider issueId="issue-1" instanceUrl="https://gitlab.example.com">
        <Probe src="https://images.example.com/public.png" />
      </GitlabMediaProvider>,
    );
    expect(view.getByRole("img")).toHaveAttribute("src", "https://images.example.com/public.png");
    expect(mocks.getGitlabMedia).not.toHaveBeenCalled();
  });
});
