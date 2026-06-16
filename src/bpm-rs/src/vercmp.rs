//! alpm/rpmvercmp version comparison — a direct port of the C `vercmp.c`.
//!
//! Compares `[epoch:]pkgver[-pkgrel]` (e.g. "16.1.0-2"). A plain string compare
//! would rank 1.10 below 1.9; this splits into alternating digit/letter runs and
//! compares run-by-run, numeric runs as integers.

use std::cmp::Ordering;

fn is_alnum(c: u8) -> bool {
    c.is_ascii_alphanumeric()
}

/// Compare one segment (epoch / ver / rel).
fn rpmvercmp(a: &str, b: &str) -> Ordering {
    if a == b {
        return Ordering::Equal;
    }
    let one = a.as_bytes();
    let two = b.as_bytes();
    let (mut i, mut j) = (0usize, 0usize);

    loop {
        while i < one.len() && !is_alnum(one[i]) {
            i += 1;
        }
        while j < two.len() && !is_alnum(two[j]) {
            j += 1;
        }
        if i >= one.len() || j >= two.len() {
            break;
        }

        let (s1, s2) = (i, j);
        let isnum = one[i].is_ascii_digit();
        if isnum {
            while i < one.len() && one[i].is_ascii_digit() {
                i += 1;
            }
            while j < two.len() && two[j].is_ascii_digit() {
                j += 1;
            }
        } else {
            while i < one.len() && one[i].is_ascii_alphabetic() {
                i += 1;
            }
            while j < two.len() && two[j].is_ascii_alphabetic() {
                j += 1;
            }
        }

        // One side started numeric where the other has an alpha run (or empty).
        if s1 == i {
            return if isnum { Ordering::Greater } else { Ordering::Less };
        }
        if s2 == j {
            return if isnum { Ordering::Greater } else { Ordering::Less };
        }

        let mut r1 = &one[s1..i];
        let mut r2 = &two[s2..j];
        if isnum {
            while r1.len() > 1 && r1[0] == b'0' {
                r1 = &r1[1..];
            }
            while r2.len() > 1 && r2[0] == b'0' {
                r2 = &r2[1..];
            }
            if r1.len() != r2.len() {
                return r1.len().cmp(&r2.len());
            }
        }
        match r1.cmp(r2) {
            Ordering::Equal => {}
            ord => return ord,
        }
    }

    // Whichever still has an alphanumeric run left is newer; a leftover alpha
    // run loses to "no run" (matches pacman's "1.0" > "1.0alpha").
    let rest1 = i < one.len();
    let rest2 = j < two.len();
    match (rest1, rest2) {
        (true, false) => {
            if one[i].is_ascii_alphabetic() {
                Ordering::Less
            } else {
                Ordering::Greater
            }
        }
        (false, true) => {
            if two[j].is_ascii_alphabetic() {
                Ordering::Greater
            } else {
                Ordering::Less
            }
        }
        _ => Ordering::Equal,
    }
}

fn split(v: &str) -> (&str, &str, &str) {
    // [epoch:]ver[-rel]
    let (epoch, rest) = match v.split_once(':') {
        Some((e, r)) => (e, r),
        None => ("0", v),
    };
    let (ver, rel) = match rest.rsplit_once('-') {
        Some((ve, re)) => (ve, re),
        None => (rest, "0"),
    };
    (epoch, ver, rel)
}

/// Full version compare over epoch, then ver, then rel.
pub fn vercmp(a: &str, b: &str) -> Ordering {
    let (ea, va, ra) = split(a);
    let (eb, vb, rb) = split(b);
    match rpmvercmp(ea, eb) {
        Ordering::Equal => {}
        o => return o,
    }
    match rpmvercmp(va, vb) {
        Ordering::Equal => {}
        o => return o,
    }
    rpmvercmp(ra, rb)
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::cmp::Ordering::*;
    #[test]
    fn ordering() {
        assert_eq!(vercmp("1.10", "1.9"), Greater);
        assert_eq!(vercmp("16.1.0-2", "16.1.0-1"), Greater);
        assert_eq!(vercmp("1.0", "1.0"), Equal);
        assert_eq!(vercmp("2.0", "1.9"), Greater);
        assert_eq!(vercmp("1:1.0", "2.0"), Greater);
        assert_eq!(vercmp("1.0-1", "1.0-2"), Less);
    }
}
