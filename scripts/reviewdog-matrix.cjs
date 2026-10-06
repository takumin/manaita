module.exports = ({ context, changes }) => {
	const targets = ["editorconfig-checker"];
	if (actions({ context, changes })) {
		targets.push("actionlint");
		targets.push("ghalint-run");
		targets.push("ghalint-act");
		targets.push("zizmor");
	}
	if (json({ context, changes })) {
		targets.push("biome");
	}
	if (yaml({ context, changes })) {
		targets.push("yamllint");
	}
	if (shell({ context, changes })) {
		targets.push("shellcheck");
		targets.push("shfmt");
	}
	if (golang({ context, changes })) {
		targets.push("gofmt");
		targets.push("gosec");
		targets.push("govet");
		targets.push("golangci-lint");
	}
	return targets;
};

function branches(context) {
	if (
		context.ref === "refs/heads/main" ||
		context.ref === "refs/heads/develop" ||
		context.ref === "refs/heads/release" ||
		context.ref.startsWith("refs/heads/releases/")
	) {
		return true;
	}
	return false;
}

function actions({ context, changes }) {
	if (
		branches(context) ||
		changes.aqua === "true" ||
		changes.reviewdog === "true" ||
		changes["github-actions"] === "true"
	) {
		return true;
	}
	return false;
}

function yaml({ context, changes }) {
	if (
		branches(context) ||
		changes.aqua === "true" ||
		changes.reviewdog === "true" ||
		changes.yaml === "true"
	) {
		return true;
	}
	return false;
}

function json({ context, changes }) {
	if (
		branches(context) ||
		changes.aqua === "true" ||
		changes.reviewdog === "true" ||
		changes.json === "true"
	) {
		return true;
	}
	return false;
}

function shell({ context, changes }) {
	if (
		branches(context) ||
		changes.aqua === "true" ||
		changes.reviewdog === "true" ||
		changes.shell === "true"
	) {
		return true;
	}
	return false;
}

function golang({ context, changes }) {
	if (
		branches(context) ||
		changes.aqua === "true" ||
		changes.reviewdog === "true" ||
		changes.go === "true"
	) {
		return true;
	}
	return false;
}
