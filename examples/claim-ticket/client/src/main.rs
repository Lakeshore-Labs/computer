use base64::{engine::general_purpose::STANDARD, Engine};
use rmcp::{model::CallToolRequestParams, transport::TokioChildProcess, ServiceExt};
use serde_json::json;
use std::{error::Error, fs, path::PathBuf};
use tokio::process::Command;

fn decode(bytes: &[u8]) -> Result<String, Box<dyn Error>> {
    let image = image::load_from_memory_with_format(bytes, image::ImageFormat::Png)?.to_luma8();
    let mut detector = quircs::Quirc::default();
    let mut symbols = Vec::new();
    for result in detector.identify(image.width() as usize, image.height() as usize, &image) {
        let symbol = result?.decode()?;
        symbols.push(String::from_utf8(symbol.payload)?);
    }
    if symbols.len() != 1 {
        return Err(format!("expected one QR symbol, got {}", symbols.len()).into());
    }
    Ok(symbols.remove(0))
}

#[tokio::main(flavor = "current_thread")]
async fn main() -> Result<(), Box<dyn Error>> {
    let args: Vec<String> = std::env::args().skip(1).collect();
    if args.len() == 2 && args[0] == "scan" {
        println!("{}", decode(&fs::read(&args[1])?)?);
        return Ok(());
    }
    if args.len() != 6 {
        return Err("pass computer, output, recipe, vendor, WASI Python directory and clockwise rotation in degrees".into());
    }
    let mut command = Command::new(&args[0]);
    command.args([
        "-mount",
        &format!("{}:/work", args[1]),
        "-mount",
        &format!("{}:/recipe:ro", args[2]),
        "-mount",
        &format!("{}:/vendor:ro", args[3]),
        "-python",
        &format!("{}/python.wasm", args[4]),
        "-python-lib",
        &format!("{}/lib", args[4]),
        "mcp",
    ]);
    let service = ().serve(TokioChildProcess::new(command)?).await?;
    let names: Vec<_> = service
        .list_all_tools()
        .await?
        .into_iter()
        .map(|t| t.name.to_string())
        .collect();
    let degrees: i16 = args[5].parse()?;
    let script = format!("set -eo pipefail\npython3 /recipe/issue.py\nmagick claim.png -background white -rotate {degrees} rotated.png\nidentify claim.png rotated.png");
    let produced = service
        .call_tool(
            CallToolRequestParams::new("bash")
                .with_arguments(json!({"command":script}).as_object().unwrap().clone()),
        )
        .await?;
    if produced.is_error == Some(true) {
        return Err(format!("Computer: {:?}", produced.content).into());
    }
    let log: String = produced
        .content
        .iter()
        .filter_map(|block| block.as_text())
        .map(|line| line.text.as_str())
        .collect();
    if !log.contains("rotated.png PNG") {
        return Err(format!("The guest did not generate both images: {log}").into());
    }
    let expected: serde_json::Value = serde_json::from_slice(&fs::read(
        PathBuf::from(&args[2]).join("lost-property.json"),
    )?)?;
    let mut scans = Vec::new();
    for name in ["claim.png", "rotated.png"] {
        let result = service
            .call_tool(
                CallToolRequestParams::new("read")
                    .with_arguments(json!({"path":name}).as_object().unwrap().clone()),
            )
            .await?;
        let image = result
            .content
            .iter()
            .find_map(|block| block.as_image())
            .ok_or("missing image content")?;
        if result.is_error == Some(true) || image.mime_type != "image/png" {
            return Err("unexpected read result".into());
        }
        let bytes = STANDARD.decode(&image.data)?;
        if bytes != fs::read(PathBuf::from(&args[1]).join(name))? {
            return Err("MCP image differs from mounted file".into());
        }
        let payload = decode(&bytes)?;
        if Some(payload.as_str()) != expected["payload"].as_str() {
            return Err("wrong decoded property reference".into());
        }
        scans.push(json!({"file":name,"bytes":bytes.len(),"decoded":payload}));
    }
    service.cancel().await?;
    println!(
        "{}",
        serde_json::to_string_pretty(
            &json!({"client":"Rust MCP SDK","degrees":degrees,"tools":names,"scans":scans})
        )?
    );
    Ok(())
}
