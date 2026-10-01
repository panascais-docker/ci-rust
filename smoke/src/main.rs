fn compress(input: &[u8]) -> Vec<u8> {
    zstd::encode_all(input, 0).expect("zstd compression failed")
}

fn main() {
    let digest = ring::digest::digest(&ring::digest::SHA256, &compress(b"ci-rust"));
    println!("{digest:?}");
}

#[cfg(test)]
mod tests {
    #[test]
    fn round_trip() {
        let compressed = super::compress(b"ci-rust");
        assert_eq!(zstd::decode_all(&compressed[..]).unwrap(), b"ci-rust");
    }
}
