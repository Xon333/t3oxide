fn main() {
    if let Err(error) = t3oxide::cli::run(std::env::args().skip(1).collect()) {
        eprintln!("t3oxide: {error:#}");
        std::process::exit(1);
    }
}
