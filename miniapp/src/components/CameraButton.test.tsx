// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, fireEvent } from "@testing-library/react";
import { CameraButton } from "./CameraButton";

afterEach(cleanup);

describe("CameraButton", () => {
  it("renders a camera-capture image input", () => {
    const { container } = render(<CameraButton onChange={() => {}} />);
    const input = container.querySelector("input[type=file]") as HTMLInputElement;
    expect(input).toBeTruthy();
    expect(input.getAttribute("capture")).toBe("environment");
    expect(input.getAttribute("accept")).toBe("image/*");
  });

  it("clicking the button opens the hidden input", () => {
    const { container } = render(<CameraButton onChange={() => {}} ariaLabel="Take photo" />);
    const input = container.querySelector("input[type=file]") as HTMLInputElement;
    const spy = vi.spyOn(input, "click");
    fireEvent.click(container.querySelector("button")!);
    expect(spy).toHaveBeenCalledTimes(1);
  });

  it("forwards the change event and clears the input value for repeat captures", () => {
    const onChange = vi.fn();
    const { container } = render(<CameraButton onChange={onChange} />);
    const input = container.querySelector("input[type=file]") as HTMLInputElement;
    fireEvent.change(input, { target: { value: "" } });
    expect(onChange).toHaveBeenCalledTimes(1);
    expect(input.value).toBe("");
  });

  it("is disabled when requested", () => {
    const { container } = render(<CameraButton onChange={() => {}} disabled />);
    expect((container.querySelector("button") as HTMLButtonElement).disabled).toBe(true);
    expect((container.querySelector("input") as HTMLInputElement).disabled).toBe(true);
  });
});
