// The parts of scripts/beta.mjs that decide, kept apart so they are tested.

const VERSION = /^(\d+)\.(\d+)\.(\d+)$/
// evenhub upload caps a changelog at 500 characters.
export const CHANGELOG_MAX = 500

export const USAGE = `Usage: pnpm -C packages/even-hub run beta [-m <changelog>] [--version x.y.z] [--create]

Packs the glasses app, uploads it to your Even developer account and puts it
on the beta channel, where the Even app's Even Hub tab installs it.

  -m <changelog>     what changed (default: the latest commit to the app)
  --version x.y.z    the version to upload (default: app.json's, patch + 1)
  --create           create the app on the account first (its first upload)

Sign in once first: pnpm -C packages/even-hub exec evenhub login`

function checkVersion(version) {
  if (!VERSION.test(version)) throw new Error(`a version is x.y.z, not ${JSON.stringify(version)}`)
  return version
}

// The next patch version of x.y.z.
export function nextVersion(version) {
  const [, major, minor, patch] = VERSION.exec(checkVersion(version))
  return `${major}.${minor}.${Number(patch) + 1}`
}

// The script's arguments, as pnpm run passes them on.
export function parseArgs(argv) {
  const args = { create: false, help: false }
  const value = (flag, i) => {
    if (i >= argv.length) throw new Error(`${flag} needs a value`)
    return argv[i]
  }
  for (let i = 0; i < argv.length; i++) {
    const flag = argv[i]
    if (flag === "--") continue
    else if (flag === "-m" || flag === "--changelog") args.changelog = value(flag, ++i)
    else if (flag === "--version") args.version = checkVersion(value(flag, ++i))
    else if (flag === "--create") args.create = true
    else if (flag === "-h" || flag === "--help") args.help = true
    else throw new Error(`unknown argument ${flag}`)
  }
  if (args.changelog !== undefined && [...args.changelog].length > CHANGELOG_MAX) {
    throw new Error(`the changelog is over ${CHANGELOG_MAX} characters`)
  }
  return args
}
