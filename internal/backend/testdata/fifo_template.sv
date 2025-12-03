// fixture fifo template
// mygo:fifo_template
module mygo_fifo #(
  parameter integer WIDTH = 32,
  parameter integer DEPTH = 1
) (
  input wire clk,
  input wire rst,
  inout wire [WIDTH-1:0] in_data,
  inout wire in_valid,
  inout wire in_ready,
  inout wire [WIDTH-1:0] out_data,
  inout wire out_valid,
  inout wire out_ready
);
endmodule
