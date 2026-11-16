//! Inactive launch preparation, not an admission capability.
//!
//! Go remains authoritative for YAML, tilde expansion, registered peers and private
//! namespace separation. TRACK_CACHE_DIR is deliberately absent: machine
//! cache_dir and platform default HOME/XDG semantics remain Go-owned. No caller may substitute this plan for ReadFixture.
use super::*;
use std::os::unix::fs::{MetadataExt, PermissionsExt};
use std::path::Path;

const CONFIG_LIMIT: u64 = 1024 * 1024;

#[derive(Clone, Debug)]
struct Binding {
    identity: FileIdentity,
    directory: bool,
}
impl Binding {
    fn capture(path: &Path, directory: bool) -> Result<Self, String> {
        if !path.is_absolute() || path.canonicalize().ok().as_deref() != Some(path) {
            return Err("Launch input must be an existing canonical absolute path".into());
        }
        let meta = std::fs::symlink_metadata(path).map_err(|e| e.to_string())?;
        if (directory && !meta.is_dir()) || (!directory && (!meta.is_file() || meta.nlink() != 1)) {
            return Err("Launch input has unsupported file type or hardlinks".into());
        }
        Ok(Self {
            identity: FileIdentity::capture(path)?,
            directory,
        })
    }
    fn verify(&self) -> Result<(), String> {
        let now = Self::capture(&self.identity.input, self.directory)?;
        if now.identity != self.identity {
            return Err("Launch input identity changed".into());
        }
        Ok(())
    }
}

/// Prepared inputs only. Deliberately not accepted by OwnedServer or Backend.
/// Exact config bytes are bounded; directory contents are not frozen. Engine
/// preflight must revalidate all resolved peers before any private cache writes.
/// Filesystem checks are point-in-time, not protection against hostile races.
#[derive(Clone, Debug)]
pub(super) struct CanonicalLaunchInputs {
    launch: LaunchConfig,
    bindings: Vec<Binding>,
    config_bytes: Vec<u8>,
    cache_context: PathBuf,
    resolution_home: PathBuf,
    resolution_cwd: PathBuf,
}
impl CanonicalLaunchInputs {
    pub(super) fn capture(
        launch: &LaunchConfig,
        cache_context: &Path,
        resolution_home: &Path,
        resolution_cwd: &Path,
    ) -> Result<Self, String> {
        launch.validate().map_err(|e| e.to_string())?;
        if launch.creation_fixture.is_some() || launch.read_fixture.is_some() {
            return Err("Canonical input plan accepts no fixture/edit authority".into());
        }
        let config = launch
            .config
            .as_deref()
            .ok_or("Explicit config is required")?;
        let private = launch
            .cache
            .as_deref()
            .ok_or("Explicit private cache is required")?;
        let mut bindings = vec![
            Binding::capture(&launch.executable, false)?,
            Binding::capture(config, false)?,
            Binding::capture(private, true)?,
            Binding::capture(cache_context, true)?,
            Binding::capture(resolution_home, true)?,
            Binding::capture(resolution_cwd, true)?,
        ];
        if private.starts_with(cache_context)
            || cache_context.starts_with(private)
            || config.starts_with(private)
        {
            return Err("Private runtime root overlaps cache resolution context or config".into());
        }
        match &launch.vault {
            Vault::Path(path) => {
                bindings.push(Binding::capture(path, true)?);
                if path.starts_with(private) || private.starts_with(path) {
                    return Err("Private runtime root overlaps active vault".into());
                }
            }
            Vault::Registered(name) => {
                if name.starts_with('-')
                    || name.len() > 256
                    || name.trim() != name
                    || name.chars().any(char::is_control)
                {
                    return Err("Invalid bounded registered selector".into());
                }
            }
        }
        let plan = Self {
            launch: launch.clone(),
            bindings,
            config_bytes: Self::read_config(config)?,
            cache_context: cache_context.to_owned(),
            resolution_home: resolution_home.to_owned(),
            resolution_cwd: resolution_cwd.to_owned(),
        };
        plan.verify()?;
        Ok(plan)
    }
    fn read_config(path: &Path) -> Result<Vec<u8>, String> {
        // Type/size preflight precedes open: static FIFO/special/oversize inputs
        // cannot block or allocate unboundedly. Concurrent hostile swaps excluded.
        Binding::capture(path, false)?;
        if std::fs::symlink_metadata(path)
            .map_err(|e| e.to_string())?
            .len()
            > CONFIG_LIMIT
        {
            return Err("Config exceeds 1 MiB".into());
        }
        let mut bytes = Vec::new();
        std::fs::File::open(path)
            .map_err(|e| e.to_string())?
            .take(CONFIG_LIMIT + 1)
            .read_to_end(&mut bytes)
            .map_err(|e| e.to_string())?;
        if bytes.len() as u64 > CONFIG_LIMIT {
            return Err("Config exceeds 1 MiB".into());
        }
        Ok(bytes)
    }
    fn verify(&self) -> Result<(), String> {
        for binding in &self.bindings {
            binding.verify()?;
        }
        if Self::read_config(self.launch.config.as_ref().unwrap())? != self.config_bytes {
            return Err("Configuration bytes changed".into());
        }
        let meta =
            std::fs::metadata(self.launch.cache.as_ref().unwrap()).map_err(|e| e.to_string())?;
        if meta.permissions().mode() & 0o077 != 0 || meta.uid() != unsafe { libc::geteuid() } {
            return Err("Private runtime root must be owned and private (0700)".into());
        }
        Ok(())
    }
    /// Configure an unspawned owned-server command. No readiness identity is
    /// invented here: the eventual owner must install its generated PID/token.
    /// This method neither opens a server nor grants ordinary-vault admission.
    pub(super) fn configure(&self, command: &mut Command) -> Result<(), String> {
        self.verify()?;
        if command.get_program() != self.launch.executable.as_os_str()
            || command.get_args().next().is_some()
        {
            return Err("Expected pristine command for captured executable".into());
        }
        clear_track_environment(command);
        command
            .current_dir(&self.resolution_cwd)
            .env("TRACK_CONFIG", self.launch.config.as_ref().unwrap())
            .env("HOME", &self.resolution_home)
            .env("XDG_CONFIG_HOME", self.launch.cache.as_ref().unwrap())
            .env("XDG_CACHE_HOME", &self.cache_context)
            .env("TMPDIR", self.launch.cache.as_ref().unwrap());
        match &self.launch.vault {
            Vault::Path(path) => {
                command.env("TRACK_VAULT", path);
            }
            Vault::Registered(name) => {
                command.args(["--vault", name]);
            }
        }
        command.args(["web", "--addr", &format!("127.0.0.1:{}", self.launch.port)]);
        self.launch
            .configure_read_mode(command)
            .map_err(|e| e.to_string())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    struct Fixture(PathBuf);
    impl Drop for Fixture {
        fn drop(&mut self) {
            let _ = std::fs::remove_dir_all(&self.0);
        }
    }
    fn fixture() -> (Fixture, LaunchConfig, PathBuf, PathBuf) {
        let root = std::env::temp_dir().join(format!(
            "track-input-plan-{}-{}",
            std::process::id(),
            NEXT_LAUNCH.fetch_add(1, Ordering::Relaxed)
        ));
        std::fs::create_dir(&root).unwrap();
        let root = root.canonicalize().unwrap();
        for name in ["vault", "private", "normal", "home"] {
            std::fs::create_dir(root.join(name)).unwrap();
        }
        std::fs::set_permissions(root.join("private"), std::fs::Permissions::from_mode(0o700))
            .unwrap();
        std::fs::write(
            root.join("engine"),
            b"not executable: no process should run",
        )
        .unwrap();
        std::fs::write(root.join("config"), b"vaults:\n  peer: ~/peer\n").unwrap();
        let mut launch =
            LaunchConfig::new(root.join("engine"), Vault::Path(root.join("vault")), 32100);
        launch.config = Some(root.join("config"));
        launch.cache = Some(root.join("private"));
        let normal = root.join("normal");
        let home = root.join("home");
        (Fixture(root), launch, normal, home)
    }
    #[test]
    fn exact_inputs_control_environment_without_granting_admission() {
        let (_f, launch, normal, home) = fixture();
        let plan = CanonicalLaunchInputs::capture(&launch, &normal, &home, &home).unwrap();
        let mut command = Command::new(&launch.executable);
        command
            .env("TRACK_VAULTS_ESCAPE", "/outside")
            .env("LD_PRELOAD", "/outside");
        plan.configure(&mut command).unwrap();
        let env: std::collections::BTreeMap<_, _> = command
            .get_envs()
            .map(|(k, v)| {
                (
                    k.to_string_lossy().into_owned(),
                    v.unwrap().to_string_lossy().into_owned(),
                )
            })
            .collect();
        assert!(!env.contains_key("TRACK_VAULTS_ESCAPE"));
        assert!(!env.contains_key("LD_PRELOAD"));
        assert_eq!(env["HOME"], home.to_string_lossy());
        assert!(!env.contains_key("TRACK_CACHE_DIR"));
        assert_eq!(env["XDG_CACHE_HOME"], normal.to_string_lossy());
        let args: Vec<_> = command
            .get_args()
            .map(|s| s.to_string_lossy().into_owned())
            .collect();
        assert!(args.contains(&"--canonical-read-only".into()));
        assert_eq!(
            args.last().unwrap(),
            &launch.cache.as_ref().unwrap().to_string_lossy()
        );
        assert!(
            matches!(OwnedServer::start(launch), Err(Error::Config(message)) if message.contains("Ordinary vault launch"))
        );
    }
    #[test]
    fn config_bytes_and_path_replacements_invalidate_plan() {
        let (f, launch, normal, home) = fixture();
        let plan = CanonicalLaunchInputs::capture(&launch, &normal, &home, &home).unwrap();
        std::fs::write(f.0.join("config"), b"vaults: {}\n").unwrap();
        assert!(
            plan.configure(&mut Command::new(&launch.executable))
                .is_err()
        );
        let plan = CanonicalLaunchInputs::capture(&launch, &normal, &home, &home).unwrap();
        std::fs::rename(&home, f.0.join("old-home")).unwrap();
        std::fs::create_dir(&home).unwrap();
        assert!(
            plan.configure(&mut Command::new(&launch.executable))
                .is_err()
        );
    }
    #[test]
    fn config_special_hardlink_symlink_and_size_fail_closed() {
        use std::os::unix::fs::symlink;
        let (f, launch, normal, home) = fixture();
        let path = f.0.join("config");
        std::fs::remove_file(&path).unwrap();
        let cpath = std::ffi::CString::new(path.as_os_str().as_encoded_bytes()).unwrap();
        assert_eq!(unsafe { libc::mkfifo(cpath.as_ptr(), 0o600) }, 0);
        assert!(CanonicalLaunchInputs::capture(&launch, &normal, &home, &home).is_err());
        std::fs::remove_file(&path).unwrap();
        std::fs::write(&path, b"{}").unwrap();
        std::fs::hard_link(&path, f.0.join("alias")).unwrap();
        assert!(CanonicalLaunchInputs::capture(&launch, &normal, &home, &home).is_err());
        std::fs::remove_file(&path).unwrap();
        symlink(f.0.join("alias"), &path).unwrap();
        assert!(CanonicalLaunchInputs::capture(&launch, &normal, &home, &home).is_err());
        std::fs::remove_file(&path).unwrap();
        let file = std::fs::File::create(&path).unwrap();
        file.set_len(CONFIG_LIMIT + 1).unwrap();
        assert!(CanonicalLaunchInputs::capture(&launch, &normal, &home, &home).is_err());
    }
    #[test]
    fn private_boundary_and_registered_selector_are_explicit() {
        let (_f, mut launch, normal, home) = fixture();
        assert!(
            CanonicalLaunchInputs::capture(&launch, launch.cache.as_ref().unwrap(), &home, &home)
                .is_err()
        );
        launch.vault = Vault::Registered("peer".into());
        let plan = CanonicalLaunchInputs::capture(&launch, &normal, &home, &home).unwrap();
        let mut cmd = Command::new(&launch.executable);
        plan.configure(&mut cmd).unwrap();
        assert_eq!(cmd.get_args().next().unwrap(), "--vault");
        for name in ["", " peer", "peer\n", "--other"] {
            launch.vault = Vault::Registered(name.into());
            assert!(CanonicalLaunchInputs::capture(&launch, &normal, &home, &home).is_err());
        }
        launch.vault = Vault::Registered("peer".into());
        std::fs::set_permissions(
            launch.cache.as_ref().unwrap(),
            std::fs::Permissions::from_mode(0o755),
        )
        .unwrap();
        assert!(
            plan.configure(&mut Command::new(&launch.executable))
                .is_err()
        );
    }
}
