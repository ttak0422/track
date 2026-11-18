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
    #[test]
    fn synthetic_config_encoding_matches_sorted_go_fixture_bytes() {
        let registry = BTreeMap::from([
            ("work".into(), PathBuf::from("/fixture/home/work")),
            ("main".into(), PathBuf::from("/fixture/home/main")),
        ]);
        assert_eq!(
            SyntheticCanonical::config_bytes(&registry, Path::new("/fixture/normal")),
            br#"{"cache_dir":"/fixture/normal","default_vault":"main","vaults":{"main":"/fixture/home/main","work":"/fixture/home/work"}}"#
        );
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
    fn synthetic() -> (Fixture, SyntheticCanonical) {
        let (f, mut launch, normal, home) = fixture();
        std::fs::create_dir(f.0.join("peer")).unwrap();
        std::fs::write(
            f.0.join(".track-native-read-fixture"),
            b"disposable-fixture-only\n",
        )
        .unwrap();
        let registry = BTreeMap::from([
            ("main".into(), f.0.join("vault")),
            ("work".into(), f.0.join("peer")),
        ]);
        std::fs::write(
            f.0.join("config"),
            SyntheticCanonical::config_bytes(&registry, &normal),
        )
        .unwrap();
        launch.vault = Vault::Registered("main".into());
        let inputs = CanonicalLaunchInputs::capture(&launch, &normal, &home, &home).unwrap();
        let proof = SyntheticCanonical::capture(&f.0, inputs, registry, &normal).unwrap();
        (f, proof)
    }
    #[test]
    fn synthetic_proof_rejects_config_change_missing_peer_and_edit_conversion() {
        let (f, proof) = synthetic();
        assert!(proof.launch().creation_fixture.is_none());
        assert!(proof.launch().read_fixture.is_none());
        assert!(OwnedServer::start(proof.launch()).is_err());
        std::fs::remove_dir(f.0.join("peer")).unwrap();
        assert!(proof.verify().is_err());
        let (f, proof) = synthetic();
        std::fs::write(
            f.0.join("config"),
            b"{}
",
        )
        .unwrap();
        assert!(proof.verify().is_err());
    }
    #[test]
    fn synthetic_registry_requires_exact_identity_and_bounded_readiness() {
        let (_f, proof) = synthetic();
        let active = serde_json::json!({"name":"main","path":proof.registry["main"]});
        let main = serde_json::json!({"name":"main","path":proof.registry["main"],"active":true});
        let work = serde_json::json!({"name":"work","path":proof.registry["work"],"active":false});
        let wire = serde_json::json!({"active":active,"vaults":[main,work],"unavailable":[]});
        assert_eq!(proof.verify_registry(&wire), Ok(true));
        let mut pending = wire.clone();
        pending["vaults"].as_array_mut().unwrap().pop();
        pending["unavailable"] =
            serde_json::json!([{"name":"work","path":proof.registry["work"],"error":"opening"}]);
        assert_eq!(proof.verify_registry(&pending), Ok(false));
        pending["unavailable"][0]["path"] = serde_json::json!("/outside");
        assert!(proof.verify_registry(&pending).is_err());
        let mut wrong = wire.clone();
        wrong["vaults"][1]["name"] = serde_json::json!("main");
        assert!(proof.verify_registry(&wrong).is_err());
        wrong = wire.clone();
        wrong["active"]["name"] = serde_json::json!("work");
        assert!(proof.verify_registry(&wrong).is_err());
        wrong = wire.clone();
        wrong["storage_state"] = serde_json::json!("nonsense");
        assert!(proof.verify_registry(&wrong).is_err());
        wrong = wire.clone();
        wrong["vaults"][1] = serde_json::json!({"name":"unexpected","active":false});
        assert!(proof.verify_registry(&wrong).is_err());
        wrong["vaults"][1]["path"] = serde_json::json!(42);
        assert!(proof.verify_registry(&wrong).is_err());
        wrong["vaults"][1]["name"] = serde_json::json!("work");
        assert!(proof.verify_registry(&wrong).is_err());
    }
    #[test]
    fn canonical_save_is_not_sent_even_when_source_looks_writable() {
        let listener = TcpListener::bind(("127.0.0.1", 0)).unwrap();
        listener.set_nonblocking(true).unwrap();
        let client = ReadClient {
            finite_only: false,
            canonical_read_only: true,
            creation_store: Default::default(),
            creation_fixture: None,
            http: reqwest::blocking::Client::new(),
            origin: format!("http://{}", listener.local_addr().unwrap()),
            alive: Arc::new(AtomicBool::new(true)),
            launch_generation: 1,
        };
        let request = SaveRequest {
            key: NoteKey {
                vault: String::new(),
                note_id: 42,
            },
            body: "replacement".into(),
            etag: "valid".into(),
            launch_generation: 1,
            storage_generation: 1,
        };
        assert!(
            matches!(client.save(&request),Err(Error::SaveNotSent(reason)) if reason.to_string().contains("Canonical read-only"))
        );
        assert_eq!(
            listener.accept().unwrap_err().kind(),
            std::io::ErrorKind::WouldBlock
        );
    }
}

/// Synthetic-only authority. Not compiled into production, and deliberately
/// uses a different marker from writable fixture permissions.
#[cfg(test)]
#[derive(Clone, Debug)]
pub(super) struct SyntheticCanonical {
    inputs: CanonicalLaunchInputs,
    root: Binding,
    marker: Binding,
    registry: BTreeMap<String, PathBuf>,
    normal_cache: Binding,
    peers: Vec<Binding>,
}
#[cfg(test)]
impl SyntheticCanonical {
    pub(super) fn config_bytes(
        registry: &BTreeMap<String, PathBuf>,
        normal_cache: &Path,
    ) -> Vec<u8> {
        // Go's fixture bridge requires exact sorted JSON bytes. GPUI enables
        // serde_json/preserve_order transitively, so json! object insertion order
        // is not a portable fixture encoding. Serialize an explicit sorted map.
        let fields = BTreeMap::from([
            ("cache_dir", serde_json::to_value(normal_cache).unwrap()),
            ("default_vault", serde_json::json!("main")),
            ("vaults", serde_json::to_value(registry).unwrap()),
        ]);
        serde_json::to_vec(&fields).unwrap()
    }
    pub(super) fn capture(
        root: &Path,
        inputs: CanonicalLaunchInputs,
        registry: BTreeMap<String, PathBuf>,
        normal_cache: &Path,
    ) -> Result<Self, String> {
        let temp = std::env::temp_dir()
            .canonicalize()
            .map_err(|e| e.to_string())?;
        if root == temp || !root.starts_with(&temp) {
            return Err("Synthetic root must be strictly inside temporary directory".into());
        }
        let root = Binding::capture(root, true)?;
        let path = &root.identity.path;
        let marker = Binding::capture(&path.join(".track-native-read-fixture"), false)?;
        if registry.is_empty() || registry.len() > 8 || !registry.contains_key("main") {
            return Err("Synthetic registry needs main and at most 8 entries".into());
        }
        let mut peers = Vec::new();
        let mut paths = std::collections::BTreeSet::new();
        for (name, peer) in &registry {
            if name.is_empty()
                || name.len() > 64
                || !name
                    .bytes()
                    .all(|c| c.is_ascii_alphanumeric() || c == b'_' || c == b'-')
                || !paths.insert(peer.clone())
                || peer == path
                || !peer.starts_with(path)
            {
                return Err("Invalid synthetic registry".into());
            }
            peers.push(Binding::capture(peer, true)?);
        }
        for binding in inputs.bindings.iter().skip(1) {
            if !binding.identity.path.starts_with(path) || binding.identity.path == *path {
                return Err("Synthetic input escaped temporary root".into());
            }
        }
        if !normal_cache.starts_with(path) || normal_cache == path {
            return Err("Synthetic normal cache escaped root".into());
        }
        let Vault::Registered(name) = &inputs.launch.vault else {
            return Err("Synthetic test requires registered selector".into());
        };
        if !registry.contains_key(name) {
            return Err("Unknown synthetic selector".into());
        }
        let proof = Self {
            inputs,
            root,
            marker,
            registry,
            normal_cache: Binding::capture(normal_cache, true)?,
            peers,
        };
        proof.verify()?;
        Ok(proof)
    }
    pub(super) fn launch(&self) -> LaunchConfig {
        self.inputs.launch.clone()
    }
    pub(super) fn verify(&self) -> Result<(), String> {
        self.verify_bindings()?;
        ReadFixture::isolated_tree(&self.root.identity.path)
    }
    // After spawn the private SQLite tree is live; do not scan transient WAL/SHM
    // entries as immutable input. Engine cache preflight owns that boundary.
    pub(super) fn verify_bindings(&self) -> Result<(), String> {
        self.inputs.verify()?;
        self.root.verify()?;
        self.marker.verify()?;
        self.normal_cache.verify()?;
        for peer in &self.peers {
            peer.verify()?;
        }
        if CanonicalLaunchInputs::read_config(&self.marker.identity.path)?
            != b"disposable-fixture-only\n"
        {
            return Err("Synthetic read marker mismatch".into());
        }
        if self.inputs.config_bytes
            != Self::config_bytes(&self.registry, &self.normal_cache.identity.path)
        {
            return Err("Synthetic configuration must exactly match generated registry".into());
        }
        Ok(())
    }
    pub(super) fn configure(&self, command: &mut Command) -> Result<(), String> {
        self.verify()?;
        self.inputs.configure(command)
    }
    pub(super) fn verify_registry(&self, value: &Value) -> Result<bool, String> {
        use super::registry_contract::{ExpectedRegistry, RegistryObservation};
        let Vault::Registered(selected) = &self.inputs.launch.vault else {
            unreachable!()
        };
        let paths = self
            .registry
            .iter()
            .map(|(name, path)| {
                path.to_str()
                    .map(|path| (name.clone(), path.to_owned()))
                    .ok_or("Non-UTF8 registry path")
            })
            .collect::<Result<BTreeMap<_, _>, _>>()?;
        let expected = ExpectedRegistry::new(selected.clone(), paths)?;
        // Legacy synthetic read path already decoded Value: reserialization
        // exercises registry shape/identity only, not raw duplicate-key checks.
        let bytes = serde_json::to_vec(value).map_err(|e| e.to_string())?;
        match expected.observe(&bytes)? {
            RegistryObservation::Initializing => Ok(false),
            // Existing synthetic harness intentionally waits for every peer;
            // production observation preserves unavailable peers independently.
            RegistryObservation::Matched { unavailable } => Ok(unavailable.is_empty()),
        }
    }
}
